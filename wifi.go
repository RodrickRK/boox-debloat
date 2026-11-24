package main

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Wi-Fi debugging. There are two ways to get the tablet onto Wi-Fi:
//
//   - Wireless debugging (Android 11+): pair once with a 6-digit code, then
//     connect. Pairing and connecting use different ports, both shown on the
//     tablet. adb usually finds and connects paired tablets by itself.
//   - From USB: while the cable is in, `adb tcpip 5555` makes the tablet
//     listen on Wi-Fi, then `adb connect <ip>:5555`. This works on any
//     Android version but lasts only until the tablet restarts.

const tcpipPort = 5555

// isWifiSerial reports whether an `adb devices` serial is a network
// connection: "192.168.1.5:5555" or an mDNS name like
// "adb-XXXX-yyyy._adb-tls-connect._tcp".
func isWifiSerial(serial string) bool {
	return strings.Contains(serial, ":") || strings.Contains(serial, "._adb-tls-connect.")
}

var hostRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]*$`)

// parseAddr checks an address typed by the user and returns host:port. With
// defPort 0 a port is required. It refuses anything adb could read as an
// option, and anything with spaces or odd characters.
func parseAddr(s string, defPort int) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "-") {
		return "", false
	}
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		if defPort == 0 {
			return "", false
		}
		host, port = s, strconv.Itoa(defPort)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", false
	}
	if ip := net.ParseIP(host); ip == nil && !hostRe.MatchString(host) {
		return "", false
	}
	return net.JoinHostPort(host, strconv.Itoa(n)), true
}

var codeRe = regexp.MustCompile(`^[0-9]{6}$`)

// connectOK reads the answer of `adb connect`, which often exits 0 even when
// it failed.
func connectOK(out string) bool {
	low := strings.ToLower(out)
	if strings.Contains(low, "failed") || strings.Contains(low, "cannot") || strings.Contains(low, "unable") {
		return false
	}
	return strings.Contains(low, "connected to")
}

// mdnsService is one line of `adb mdns services`.
type mdnsService struct {
	Name string `json:"name"`
	Kind string `json:"kind"` // pair or connect
	Addr string `json:"addr"`
}

func parseMdns(out string) []mdnsService {
	var list []mdnsService
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		kind := ""
		switch {
		case strings.HasPrefix(f[1], "_adb-tls-pairing."):
			kind = "pair"
		case strings.HasPrefix(f[1], "_adb-tls-connect."):
			kind = "connect"
		default:
			continue
		}
		if addr, ok := parseAddr(f[2], 0); ok {
			list = append(list, mdnsService{Name: f[0], Kind: kind, Addr: addr})
		}
	}
	return list
}

var inetRe = regexp.MustCompile(`inet (\d+\.\d+\.\d+\.\d+)/`)

// wifiIP asks the tablet for its Wi-Fi address.
func (s *Server) wifiIP(serial string) string {
	out, _ := s.be.Shell(serial, "ip", "-f", "inet", "addr", "show", "wlan0")
	if m := inetRe.FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return ""
}

// sameTablet picks one entry when the tablet shows up twice, over USB and
// over Wi-Fi. It returns false when the entries are different devices.
// USB wins: it's faster. Caller holds s.mu.
func (s *Server) sameTablet(ready []DeviceEntry) (DeviceEntry, bool) {
	first := ""
	for _, d := range ready {
		id := s.prop(d.Serial, "ro.serialno")
		if id == "" || (first != "" && id != first) {
			return DeviceEntry{}, false
		}
		first = id
	}
	for _, d := range ready {
		if !isWifiSerial(d.Serial) {
			return d, true
		}
	}
	return ready[0], true
}

func decodeBody(r *http.Request, v any) bool {
	return json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(v) == nil
}

// handleWifiScan lists tablets that announce wireless debugging on the
// network, so the user doesn't have to type addresses. It's best effort:
// some networks and adb versions don't support it.
func (s *Server) handleWifiScan(w http.ResponseWriter, r *http.Request) {
	list := []mdnsService{}
	if s.be.Available() {
		if out, err := s.be.MdnsServices(); err == nil {
			list = append(list, parseMdns(out)...)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"services": list})
}

// reach tries a plain TCP connection to the tablet. It's only used after
// adb fails, to explain why: adb 37 reports every pairing failure as
// "protocol fault (couldn't read status message)", whether the code was
// wrong or the tablet couldn't be reached at all. It never blocks adb: on a
// Mac, this process and adb's server can have different Local Network
// permission. Android's pairing and connect servers both shrug off a
// connection that closes straight away.
var reach = func(addr string) error {
	c, err := net.DialTimeout("tcp", addr, 4*time.Second)
	if err != nil {
		return err
	}
	return c.Close()
}

// unreachableMsg explains why this computer can't reach the tablet.
func unreachableMsg(addr string, err error) string {
	msg := "This computer can't reach the tablet at " + addr + ". Check that both are on the same Wi-Fi network, the tablet is awake, and the address and port match what it shows now."
	low := strings.ToLower(err.Error())
	if runtime.GOOS == "darwin" && strings.Contains(low, "no route to host") {
		msg += " On a Mac this usually means the app that started boox-debloat (like Terminal) isn't allowed to reach local devices: turn it on in System Settings › Privacy & Security › Local Network, then try again."
	} else if strings.Contains(low, "refused") {
		msg = "The tablet is there, but nothing is listening on " + addr + ". The port changes each time; check it on the tablet and keep that screen open."
	}
	return msg
}

// freshServer restarts adb's background server when no device is connected,
// so nothing drops. An adb server started long ago, from an older adb or
// by an app the Mac doesn't let reach local devices, fails every Wi-Fi
// attempt even when this app can reach the tablet. Caller holds s.mu.
func (s *Server) freshServer() bool {
	if list, err := s.be.Devices(); err != nil || len(list) > 0 {
		return false
	}
	if err := s.be.RestartServer(); err != nil {
		return false
	}
	s.log.add("Restarted adb's background server and tried again")
	return true
}

// connectAfterPair waits for a just-paired tablet to announce its connect
// address on the network, then connects to it. adb sometimes connects by
// itself, but leaves the tablet "offline".
func (s *Server) connectAfterPair(host string) string {
	for i := 0; i < 6; i++ {
		if out, err := s.be.MdnsServices(); err == nil {
			for _, svc := range parseMdns(out) {
				if svc.Kind != "connect" || !strings.HasPrefix(svc.Addr, host+":") {
					continue
				}
				if cout, err := s.be.Connect(svc.Addr); err == nil && connectOK(cout) {
					return svc.Addr
				}
			}
		}
		time.Sleep(time.Second)
	}
	return ""
}

// cleanAddr drops spaces, so "192.168.1.20 : 37145" pasted from the tablet
// still works.
func cleanAddr(s string) string {
	return strings.Join(strings.Fields(s), "")
}

// handleWifiPair runs `adb pair`, then connects. It holds s.mu so two
// presses can't pair at once and the adb server isn't restarted under
// another command.
func (s *Server) handleWifiPair(w http.ResponseWriter, r *http.Request) {
	var body struct{ Addr, Code string }
	if !decodeBody(r, &body) {
		writeErr(w, http.StatusBadRequest, "Bad request.")
		return
	}
	addr, ok := parseAddr(cleanAddr(body.Addr), 0)
	if !ok {
		writeErr(w, http.StatusBadRequest, "Type the address and port shown under Pair device with pairing code, like 192.168.1.20:37145.")
		return
	}
	code := strings.Join(strings.Fields(body.Code), "")
	if !codeRe.MatchString(code) {
		writeErr(w, http.StatusBadRequest, "The pairing code is the 6 digits shown on the tablet.")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.be.Available() {
		writeErr(w, http.StatusConflict, "Install Android platform tools first.")
		return
	}
	paired := func(out string, err error) bool {
		return err == nil && strings.Contains(strings.ToLower(out), "successfully paired")
	}
	out, err := s.be.Pair(addr, code)
	if !paired(out, err) && strings.Contains(strings.ToLower(out), "unknown command") {
		writeErr(w, http.StatusBadGateway, "Your adb is too old to pair. Update Android platform tools, then try again.")
		return
	}
	if !paired(out, err) {
		// This app can reach the tablet but adb couldn't: a stale adb server.
		probe := reach(addr)
		if probe == nil && s.freshServer() {
			out, err = s.be.Pair(addr, code)
		}
		if !paired(out, err) {
			if probe != nil {
				writeErr(w, http.StatusBadGateway, unreachableMsg(addr, probe))
			} else {
				writeErr(w, http.StatusBadGateway, "The tablet answered, but pairing didn't work. Check the 6-digit code, and keep the Pair device with pairing code window open on the tablet while you press Pair.")
			}
			return
		}
	}
	host, _, _ := net.SplitHostPort(addr)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "connected": s.connectAfterPair(host)})
}

// handleWifiConnect runs `adb connect`. Port 5555 is assumed when none is
// typed.
func (s *Server) handleWifiConnect(w http.ResponseWriter, r *http.Request) {
	var body struct{ Addr string }
	if !decodeBody(r, &body) {
		writeErr(w, http.StatusBadRequest, "Bad request.")
		return
	}
	addr, ok := parseAddr(cleanAddr(body.Addr), tcpipPort)
	if !ok {
		writeErr(w, http.StatusBadRequest, "Type the tablet's address, like 192.168.1.20:41027.")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.be.Available() {
		writeErr(w, http.StatusConflict, "Install Android platform tools first.")
		return
	}
	out, err := s.be.Connect(addr)
	if err != nil || !connectOK(out) {
		if strings.Contains(strings.ToLower(out), "authenticate") {
			writeErr(w, http.StatusBadGateway, "The tablet hasn't trusted this computer yet. Pair it with a code first, or allow the prompt on the tablet.")
			return
		}
		probe := reach(addr)
		if probe == nil && s.freshServer() {
			out, err = s.be.Connect(addr)
		}
		if err != nil || !connectOK(out) {
			if probe != nil {
				writeErr(w, http.StatusBadGateway, unreachableMsg(addr, probe))
			} else {
				writeErr(w, http.StatusBadGateway, "The tablet answered, but adb couldn't connect to "+addr+". Use the IP address & Port from the Wireless debugging screen, not the pairing one.")
			}
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "addr": addr})
}

// handleWifiSwitch moves a USB connection onto Wi-Fi: it reads the tablet's
// Wi-Fi address, runs `adb tcpip 5555`, then connects to it.
func (s *Server) handleWifiSwitch(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dev, ok := s.readyDevice(w, false)
	if !ok {
		return
	}
	if isWifiSerial(dev.Serial) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "addr": dev.Serial, "already": true})
		return
	}
	ip := s.wifiIP(dev.Serial)
	if ip == "" {
		writeErr(w, http.StatusConflict, "The tablet isn't on Wi-Fi. Turn on Wi-Fi on the tablet, join the same network as this computer, and try again.")
		return
	}
	if _, err := s.be.TCPIP(dev.Serial, tcpipPort); err != nil {
		writeErr(w, http.StatusBadGateway, "Couldn't turn on Wi-Fi debugging: "+err.Error())
		return
	}
	addr := net.JoinHostPort(ip, strconv.Itoa(tcpipPort))
	// The tablet restarts adb on its side; give it a moment.
	for i := 0; i < 5; i++ {
		time.Sleep(time.Duration(800+i*400) * time.Millisecond)
		if out, err := s.be.Connect(addr); err == nil && connectOK(out) {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "addr": addr})
			return
		}
	}
	writeErr(w, http.StatusBadGateway, "The tablet is listening on "+addr+" but this computer couldn't reach it. Check that both are on the same Wi-Fi network, then connect to that address below.")
}
