package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Backend talks to Android devices. The real one runs adb; the demo one
// pretends to be a C69 M Star so the UI can be tried without a tablet.
type Backend interface {
	Available() bool
	Devices() ([]DeviceEntry, error)
	Shell(serial string, args ...string) (string, error)
	Push(serial, local, remote string) error
	Reboot(serial string) error
}

// DeviceEntry is one line of `adb devices`.
type DeviceEntry struct {
	Serial string
	State  string // device, unauthorized, offline, ...
}

// ---------------------------------------------------------------------------
// Real adb
// ---------------------------------------------------------------------------

type adbBackend struct {
	path string
}

// findAdb looks for adb on PATH and in the usual install locations.
func findAdb() string {
	if p, err := exec.LookPath("adb"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	var candidates []string
	switch runtime.GOOS {
	case "windows":
		local := os.Getenv("LOCALAPPDATA")
		candidates = []string{
			filepath.Join(home, "scoop", "shims", "adb.exe"),
			filepath.Join(local, "Android", "Sdk", "platform-tools", "adb.exe"),
			filepath.Join(local, "Microsoft", "WinGet", "Links", "adb.exe"),
		}
	case "darwin":
		candidates = []string{
			"/opt/homebrew/bin/adb",
			"/usr/local/bin/adb",
			filepath.Join(home, "Library", "Android", "sdk", "platform-tools", "adb"),
		}
	default:
		candidates = []string{
			"/usr/bin/adb",
			filepath.Join(home, "Android", "Sdk", "platform-tools", "adb"),
		}
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

func (a *adbBackend) Available() bool { return a.path != "" }

func (a *adbBackend) run(timeout time.Duration, args ...string) (string, error) {
	if a.path == "" {
		return "", errors.New("adb is not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, a.path, args...).CombinedOutput()
	text := strings.ReplaceAll(string(out), "\r", "")
	if ctx.Err() == context.DeadlineExceeded {
		return text, errors.New("the tablet stopped responding")
	}
	return text, err
}

func (a *adbBackend) Devices() ([]DeviceEntry, error) {
	out, err := a.run(15*time.Second, "devices")
	if err != nil {
		return nil, err
	}
	return parseDevices(out), nil
}

func parseDevices(out string) []DeviceEntry {
	var list []DeviceEntry
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || strings.HasPrefix(line, "List of devices") || strings.HasPrefix(line, "*") {
			continue
		}
		list = append(list, DeviceEntry{Serial: f[0], State: f[1]})
	}
	return list
}

func (a *adbBackend) Shell(serial string, args ...string) (string, error) {
	full := append([]string{"-s", serial, "shell"}, args...)
	return a.run(60*time.Second, full...)
}

func (a *adbBackend) Push(serial, local, remote string) error {
	out, err := a.run(120*time.Second, "-s", serial, "push", local, remote)
	if err != nil {
		return errors.New(strings.TrimSpace(out))
	}
	return nil
}

func (a *adbBackend) Reboot(serial string) error {
	_, err := a.run(30*time.Second, "-s", serial, "reboot")
	return err
}

// ---------------------------------------------------------------------------
// Demo backend
// ---------------------------------------------------------------------------

type demoBackend struct {
	state map[string]string // package -> enabled | disabled | removed
}

func newDemoBackend(c *Catalog) *demoBackend {
	d := &demoBackend{state: map[string]string{}}
	for i, a := range c.Remove {
		if i%3 == 0 {
			d.state[a.ID] = "removed"
		} else {
			d.state[a.ID] = "enabled"
		}
	}
	for _, a := range c.Disable {
		d.state[a.ID] = "enabled"
	}
	for _, a := range c.Protected {
		d.state[a.ID] = "enabled"
	}
	// Show the most common problem: ksync removed by an older guide.
	d.state["com.onyx.android.ksync"] = "removed"
	for _, p := range []string{"android", "com.android.settings", "com.android.systemui", "org.koreader.launcher"} {
		d.state[p] = "enabled"
	}
	return d
}

func (d *demoBackend) Available() bool { return true }

func (d *demoBackend) Devices() ([]DeviceEntry, error) {
	return []DeviceEntry{{Serial: "DEMO-C69", State: "device"}}, nil
}

func (d *demoBackend) Shell(serial string, args ...string) (string, error) {
	time.Sleep(120 * time.Millisecond)
	cmd := strings.Join(args, " ")
	last := ""
	if len(args) > 0 {
		last = args[len(args)-1]
	}
	switch {
	case cmd == "getprop ro.product.model":
		return "C69_M_STAR\n", nil
	case cmd == "getprop ro.build.display.id":
		return "2023-07-20_17-44_3.2.2-bridge_1d80e860a\n", nil
	case cmd == "getprop ro.build.version.release":
		return "11\n", nil
	case strings.HasPrefix(cmd, "pm list packages"):
		var b strings.Builder
		for p, s := range d.state {
			show := false
			switch {
			case strings.HasSuffix(cmd, " -u"):
				show = true
			case strings.HasSuffix(cmd, " -e"):
				show = s == "enabled"
			case strings.HasSuffix(cmd, " -d"):
				show = s == "disabled"
			default:
				show = s != "removed"
			}
			if show {
				b.WriteString("package:" + p + "\n")
			}
		}
		return b.String(), nil
	case strings.HasPrefix(cmd, "pm uninstall --user 0 "):
		d.state[last] = "removed"
		return "Success\n", nil
	case strings.HasPrefix(cmd, "cmd package install-existing "):
		d.state[last] = "enabled"
		return "Package " + last + " installed for user: 0\n", nil
	case strings.HasPrefix(cmd, "pm enable --user 0 "):
		d.state[last] = "enabled"
		return "Package " + last + " new state: enabled\n", nil
	case strings.HasPrefix(cmd, "pm disable-user --user 0 "):
		d.state[last] = "disabled"
		return "Package " + last + " new state: disabled-user\n", nil
	case strings.HasPrefix(cmd, "am broadcast"):
		return "Broadcast completed: result=0\n", nil
	}
	return "", nil
}

func (d *demoBackend) Push(serial, local, remote string) error {
	time.Sleep(400 * time.Millisecond)
	return nil
}

func (d *demoBackend) Reboot(serial string) error { return nil }
