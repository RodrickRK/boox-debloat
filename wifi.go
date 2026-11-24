package main

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"regexp"
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

// handleWifiPair runs `adb pair`. It doesn't hold s.mu: pairing waits on
// the network, and status checks should keep answering meanwhile.
func (s *Server) handleWifiPair(w http.ResponseWriter, r *http.Request) {
	var body struct{ Addr, Code string }
	if !decodeBody(r, &body) {
		writeErr(w, http.StatusBadRequest, "Bad request.")
		return
	}
	addr, ok := parseAddr(body.Addr, 0)
	if !ok {
		writeErr(w, http.StatusBadRequest, "Type the address and port shown under Pair device with pairing code, like 192.168.1.20:37145.")
		return
	}
	code := strings.ReplaceAll(strings.TrimSpace(body.Code), " ", "")
	if !codeRe.MatchString(code) {
		writeErr(w, http.StatusBadRequest, "The pairing code is the 6 digits shown on the tablet.")
		return
	}
	if !s.be.Available() {
		writeErr(w, http.StatusConflict, "Install Android platform tools first.")
		return
	}
	out, err := s.be.Pair(addr, code)
	if err != nil || !strings.Contains(strings.ToLower(out), "successfully paired") {
		msg := "Pairing didn't work. Check the code and that the pairing window is still open on the tablet, then try again."
		if strings.Contains(strings.ToLower(out), "unknown command") {
			msg = "Your adb is too old to pair. Update Android platform tools, then try again."
		}
		writeErr(w, http.StatusBadGateway, msg)
		return
	}
	// adb usually connects by itself once paired. Help it along when the
	// tablet announces its connect port on the network.
	host, _, _ := net.SplitHostPort(addr)
	connected := ""
	if mout, err := s.be.MdnsServices(); err == nil {
		for _, svc := range parseMdns(mout) {
			if svc.Kind != "connect" || !strings.HasPrefix(svc.Addr, host+":") {
				continue
			}
			if cout, err := s.be.Connect(svc.Addr); err == nil && connectOK(cout) {
				connected = svc.Addr
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "connected": connected})
}

// handleWifiConnect runs `adb connect`. Port 5555 is assumed when none is
// typed. Like pairing, it doesn't hold s.mu.
func (s *Server) handleWifiConnect(w http.ResponseWriter, r *http.Request) {
	var body struct{ Addr string }
	if !decodeBody(r, &body) {
		writeErr(w, http.StatusBadRequest, "Bad request.")
		return
	}
	addr, ok := parseAddr(body.Addr, tcpipPort)
	if !ok {
		writeErr(w, http.StatusBadRequest, "Type the tablet's address, like 192.168.1.20:41027.")
		return
	}
	if !s.be.Available() {
		writeErr(w, http.StatusConflict, "Install Android platform tools first.")
		return
	}
	out, err := s.be.Connect(addr)
	if err != nil || !connectOK(out) {
		msg := "Couldn't reach the tablet at " + addr + ". Check that it's on the same Wi-Fi as this computer and that the address and port match what the tablet shows."
		if strings.Contains(strings.ToLower(out), "authenticate") {
			msg = "The tablet hasn't trusted this computer yet. Pair it with a code first, or allow the prompt on the tablet."
		}
		writeErr(w, http.StatusBadGateway, msg)
		return
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
