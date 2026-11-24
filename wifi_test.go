package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseAddr(t *testing.T) {
	cases := []struct {
		in      string
		defPort int
		want    string
		ok      bool
	}{
		{"192.168.1.20:37145", 0, "192.168.1.20:37145", true},
		{" 192.168.1.20:5555 ", 0, "192.168.1.20:5555", true},
		{"192.168.1.20", 0, "", false},
		{"192.168.1.20", 5555, "192.168.1.20:5555", true},
		{"tablet.local:5555", 0, "tablet.local:5555", true},
		{"[fe80::1]:5555", 0, "[fe80::1]:5555", true},
		{"192.168.1.20:0", 0, "", false},
		{"192.168.1.20:70000", 0, "", false},
		{"-s:5555", 0, "", false},
		{"--help", 5555, "", false},
		{"a b:5555", 0, "", false},
		{"host;rm:5555", 0, "", false},
		{"", 5555, "", false},
	}
	for _, c := range cases {
		got, ok := parseAddr(c.in, c.defPort)
		if got != c.want || ok != c.ok {
			t.Errorf("parseAddr(%q, %d) = %q, %v; want %q, %v", c.in, c.defPort, got, ok, c.want, c.ok)
		}
	}
}

func TestConnectOK(t *testing.T) {
	good := []string{"connected to 192.168.1.20:5555\n", "already connected to 192.168.1.20:5555\n"}
	bad := []string{
		"failed to connect to '192.168.1.20:5555': Connection refused\n",
		"cannot connect to 192.168.1.20:5555: No route to host\n",
		"failed to authenticate to 192.168.1.20:5555\n",
		"",
	}
	for _, s := range good {
		if !connectOK(s) {
			t.Errorf("connectOK(%q) = false", s)
		}
	}
	for _, s := range bad {
		if connectOK(s) {
			t.Errorf("connectOK(%q) = true", s)
		}
	}
}

func TestParseMdns(t *testing.T) {
	out := "List of discovered mdns services\n" +
		"adb-1234-abcd\t_adb-tls-pairing._tcp\t192.168.1.20:37145\n" +
		"adb-1234-abcd\t_adb-tls-connect._tcp\t192.168.1.20:41027\n" +
		"adb-9999\t_adb._tcp\t192.168.1.30:5555\n" +
		"junk\t_adb-tls-connect._tcp\tnot-an-address\n"
	got := parseMdns(out)
	if len(got) != 2 || got[0].Kind != "pair" || got[0].Addr != "192.168.1.20:37145" || got[1].Kind != "connect" || got[1].Addr != "192.168.1.20:41027" {
		t.Fatalf("parseMdns = %+v", got)
	}
}

func TestIsWifiSerial(t *testing.T) {
	for s, want := range map[string]bool{
		"192.168.1.20:5555":                    true,
		"adb-1234-abcd._adb-tls-connect._tcp":  true,
		"adb-1234-abcd._adb-tls-connect._tcp.": true,
		"7C0A1B2C":                             false,
		"emulator-5554":                        false,
	} {
		if got := isWifiSerial(s); got != want {
			t.Errorf("isWifiSerial(%q) = %v", s, got)
		}
	}
}

// twoWayBackend shows tablets over USB and Wi-Fi, each with a serial number.
type twoWayBackend struct {
	*demoBackend
	entries   []DeviceEntry
	serialNos map[string]string
	connected []string
}

func (b *twoWayBackend) Devices() ([]DeviceEntry, error) { return b.entries, nil }

func (b *twoWayBackend) Shell(serial string, args ...string) (string, error) {
	switch strings.Join(args, " ") {
	case "getprop ro.serialno":
		return b.serialNos[serial] + "\n", nil
	case "ip -f inet addr show wlan0":
		return "30: wlan0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500\n    inet 192.168.1.20/24 brd 192.168.1.255 scope global wlan0\n", nil
	}
	return b.demoBackend.Shell(serial, args...)
}

func (b *twoWayBackend) Connect(addr string) (string, error) {
	b.connected = append(b.connected, addr)
	return "connected to " + addr + "\n", nil
}

func testServer(t *testing.T, be Backend) *Server {
	t.Helper()
	return &Server{cat: testCatalog(t), be: be, token: "t", host: "127.0.0.1:1", log: newActivityLog()}
}

func TestSameTabletOverUSBAndWifi(t *testing.T) {
	be := &twoWayBackend{
		demoBackend: newDemoBackend(testCatalog(t)),
		entries:     []DeviceEntry{{"192.168.1.20:5555", "device"}, {"USB123", "device"}},
		serialNos:   map[string]string{"192.168.1.20:5555": "USB123", "USB123": "USB123"},
	}
	s := testServer(t, be)
	dev, state, _ := s.pickDevice()
	if state != "device" || dev.Serial != "USB123" {
		t.Fatalf("got %q %q, want the USB entry", dev.Serial, state)
	}

	be.serialNos["192.168.1.20:5555"] = "OTHER"
	if _, state, _ := s.pickDevice(); state != "multiple" {
		t.Fatalf("two different tablets: got %q, want multiple", state)
	}
}

func call(s *Server, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = s.host
	req.Header.Set("X-Token", s.token)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	return rec
}

// reachable makes the network check pass for the test's made-up addresses.
func reachable(t *testing.T) {
	old := reach
	reach = func(string) error { return nil }
	t.Cleanup(func() { reach = old })
}

func TestWifiEndpoints(t *testing.T) {
	reachable(t)
	s := testServer(t, newDemoBackend(testCatalog(t)))

	if rec := call(s, http.MethodPost, "/api/wifi/pair", `{"addr":"192.168.1.42:37145","code":"123456"}`); rec.Code != http.StatusOK {
		t.Errorf("pair: %d %s", rec.Code, rec.Body)
	}
	if rec := call(s, http.MethodPost, "/api/wifi/pair", `{"addr":"192.168.1.42:37145","code":"12ab"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("pair with a bad code: %d", rec.Code)
	}
	if rec := call(s, http.MethodPost, "/api/wifi/connect", `{"addr":"192.168.1.42"}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "192.168.1.42:5555") {
		t.Errorf("connect: %d %s", rec.Code, rec.Body)
	}
	if rec := call(s, http.MethodPost, "/api/wifi/connect", `{"addr":"--help"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("connect to an option: %d", rec.Code)
	}
	if rec := call(s, http.MethodGet, "/api/wifi/scan", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"kind":"pair"`) {
		t.Errorf("scan: %d %s", rec.Code, rec.Body)
	}
}

func TestWifiSwitch(t *testing.T) {
	be := &twoWayBackend{
		demoBackend: newDemoBackend(testCatalog(t)),
		entries:     []DeviceEntry{{"USB123", "device"}},
		serialNos:   map[string]string{"USB123": "USB123"},
	}
	s := testServer(t, be)
	rec := call(s, http.MethodPost, "/api/wifi/switch", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "192.168.1.20:5555") {
		t.Fatalf("switch: %d %s", rec.Code, rec.Body)
	}
	if len(be.connected) != 1 || be.connected[0] != "192.168.1.20:5555" {
		t.Fatalf("connected to %v", be.connected)
	}
}

// flakyPair fails pairing until adb's server is restarted, like a stale
// server the Mac won't let reach local devices.
type flakyPair struct {
	*demoBackend
	restarted bool
	pairs     int
	devices   []DeviceEntry
	mdns      string
}

func (b *flakyPair) Devices() ([]DeviceEntry, error) { return b.devices, nil }
func (b *flakyPair) RestartServer() error            { b.restarted = true; return nil }
func (b *flakyPair) MdnsServices() (string, error)   { return b.mdns, nil }
func (b *flakyPair) Pair(addr, code string) (string, error) {
	b.pairs++
	if !b.restarted {
		return "error: protocol fault (couldn't read status message): Undefined error: 0\n", errors.New("exit status 1")
	}
	return "Successfully paired to " + addr + "\n", nil
}

func TestPairRestartsStaleServerAndConnects(t *testing.T) {
	reachable(t)
	be := &flakyPair{
		demoBackend: newDemoBackend(testCatalog(t)),
		mdns:        "adb-X\t_adb-tls-connect._tcp\t192.168.29.101:44157\n",
	}
	s := testServer(t, be)
	rec := call(s, http.MethodPost, "/api/wifi/pair", `{"addr":"192.168.29.101 : 37921","code":"739 313"}`)
	if rec.Code != http.StatusOK || !be.restarted || be.pairs != 2 {
		t.Fatalf("got %d %s, restarted=%v pairs=%d", rec.Code, rec.Body, be.restarted, be.pairs)
	}
	if !strings.Contains(rec.Body.String(), `"connected":"192.168.29.101:44157"`) {
		t.Fatalf("should connect to the announced address: %s", rec.Body)
	}
}

func TestPairKeepsConnectedDevices(t *testing.T) {
	reachable(t)
	be := &flakyPair{
		demoBackend: newDemoBackend(testCatalog(t)),
		devices:     []DeviceEntry{{"USB123", "device"}},
	}
	s := testServer(t, be)
	if rec := call(s, http.MethodPost, "/api/wifi/pair", `{"addr":"192.168.29.101:37921","code":"739313"}`); rec.Code != http.StatusBadGateway {
		t.Fatalf("got %d", rec.Code)
	}
	if be.restarted {
		t.Fatal("must not restart adb's server while a device is connected")
	}
}

func TestPairUnreachable(t *testing.T) {
	old := reach
	reach = func(string) error { return errors.New("dial tcp 192.168.29.101:37921: connect: no route to host") }
	t.Cleanup(func() { reach = old })
	be := &flakyPair{demoBackend: newDemoBackend(testCatalog(t))}
	s := testServer(t, be)
	rec := call(s, http.MethodPost, "/api/wifi/pair", `{"addr":"192.168.29.101:37921","code":"739313"}`)
	// adb is always tried first; the network check only explains the failure.
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "can't reach the tablet") || be.pairs != 1 || be.restarted {
		t.Fatalf("got %d %s pairs=%d", rec.Code, rec.Body, be.pairs)
	}
}
