package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
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
	Pull(serial, remote, local string) error
	Install(serial, apk string) (string, error)
	Reboot(serial string) error
}

// Copying big files and installing apps can take a while.
const transferTimeout = 15 * time.Minute

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
	// The copy the app downloaded itself comes first.
	candidates := []string{filepath.Join(platformToolsDir(), adbExe())}
	switch runtime.GOOS {
	case "windows":
		local := os.Getenv("LOCALAPPDATA")
		candidates = append(candidates,
			filepath.Join(home, "scoop", "shims", "adb.exe"),
			filepath.Join(local, "Android", "Sdk", "platform-tools", "adb.exe"),
			filepath.Join(local, "Microsoft", "WinGet", "Links", "adb.exe"),
		)
	case "darwin":
		candidates = append(candidates,
			"/opt/homebrew/bin/adb",
			"/usr/local/bin/adb",
			filepath.Join(home, "Library", "Android", "sdk", "platform-tools", "adb"),
		)
	default:
		candidates = append(candidates,
			"/usr/bin/adb",
			filepath.Join(home, "Android", "Sdk", "platform-tools", "adb"),
		)
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

// Available reports whether adb can be used. If it wasn't found at start,
// it looks again, so installing adb while the app is open just works.
func (a *adbBackend) Available() bool {
	if a.path == "" {
		a.path = findAdb()
	}
	return a.path != ""
}

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
		state := f[1]
		// Linux without udev rules: "SERIAL	no permissions (user ... udev rules?) ..."
		if state == "no" && len(f) > 2 && strings.HasPrefix(f[2], "permissions") {
			state = "nopermission"
		}
		list = append(list, DeviceEntry{Serial: f[0], State: state})
	}
	return list
}

func (a *adbBackend) Shell(serial string, args ...string) (string, error) {
	full := append([]string{"-s", serial, "shell"}, args...)
	return a.run(60*time.Second, full...)
}

func (a *adbBackend) Push(serial, local, remote string) error {
	out, err := a.run(transferTimeout, "-s", serial, "push", local, remote)
	if err != nil {
		return errors.New(strings.TrimSpace(out))
	}
	return nil
}

func (a *adbBackend) Pull(serial, remote, local string) error {
	out, err := a.run(transferTimeout, "-s", serial, "pull", remote, local)
	if err != nil {
		return errors.New(strings.TrimSpace(out))
	}
	return nil
}

// Install runs `adb install -r`, which installs or updates an app.
func (a *adbBackend) Install(serial, apk string) (string, error) {
	return a.run(transferTimeout, "-s", serial, "install", "-r", apk)
}

func (a *adbBackend) Reboot(serial string) error {
	_, err := a.run(30*time.Second, "-s", serial, "reboot")
	return err
}

// ---------------------------------------------------------------------------
// Demo backend
// ---------------------------------------------------------------------------

type demoBackend struct {
	state map[string]string   // package -> enabled | disabled | removed
	user  map[string]string   // apps you installed: package -> version
	files map[string][]string // folder -> "d name" or "f name size"
}

func newDemoBackend(c *Catalog) *demoBackend {
	d := &demoBackend{
		state: map[string]string{},
		user: map[string]string{
			"org.koreader.launcher.fdroid": "2025.10",
			"info.plateaukao.einkbro":      "14.2.0",
			"com.aurora.store":             "4.8.4",
			"org.fdroid.fdroid":            "1.23.1",
		},
		files: map[string][]string{
			"/sdcard":              {"d Alarms", "d Android", "d Books", "d Download", "d Pictures", "d Empty folder"},
			"/sdcard/Books":        {"f Pride and Prejudice.epub 512340", "f The Odyssey.pdf 4823010", "d Manga"},
			"/sdcard/Books/Manga":  {},
			"/sdcard/Download":     {"f KOReader-v2025.10.apk 41230000", "f notes.txt 1204"},
			"/sdcard/Pictures":     {"f screensaver-sleep.png 302114"},
			"/sdcard/Alarms":       {},
			"/sdcard/Android":      {"d data", "d media", "d obb"},
			"/sdcard/Empty folder": {},
		},
	}
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
	// Boox ships Google Play turned off until "Enable Google Play" is used.
	for _, p := range playPackages {
		d.state[p] = "disabled"
	}
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
	case cmd == "getprop ro.product.cpu.abi":
		return "arm64-v8a\n", nil
	case cmd == "getprop ro.build.version.release":
		return "11\n", nil
	case cmd == userAppsCmd:
		var b strings.Builder
		for p, v := range d.user {
			b.WriteString(p + " versionName=" + v + "\n")
		}
		return b.String(), nil
	case cmd == "pm list packages -3":
		var b strings.Builder
		for p := range d.user {
			b.WriteString("package:" + p + "\n")
		}
		return b.String(), nil
	case strings.HasPrefix(cmd, "pm uninstall ") && !strings.HasPrefix(cmd, "pm uninstall --user"):
		if _, ok := d.user[last]; !ok {
			return "Failure [DELETE_FAILED_INTERNAL_ERROR]\n", nil
		}
		delete(d.user, last)
		return "Success\n", nil
	case strings.HasPrefix(cmd, "ls -la "):
		return d.ls(unquote(strings.TrimPrefix(cmd, "ls -la ")))
	case strings.HasPrefix(cmd, "if [ -d "):
		return d.remove(cmd)
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
	dir, name := path.Split(remote)
	dir = strings.TrimSuffix(dir, "/")
	if _, ok := d.files[dir]; ok {
		size := int64(0)
		if st, err := os.Stat(local); err == nil {
			size = st.Size()
		}
		d.files[dir] = append(d.dropFile(dir, name), fmt.Sprintf("f %s %d", name, size))
	}
	return nil
}

func (d *demoBackend) Pull(serial, remote, local string) error {
	time.Sleep(200 * time.Millisecond)
	return os.WriteFile(local, []byte("Demo file: "+remote+"\n"), 0o644)
}

func (d *demoBackend) Install(serial, apk string) (string, error) {
	time.Sleep(600 * time.Millisecond)
	d.user["com.example.demo"+strconv.Itoa(len(d.user))] = "1.0"
	return "Performing Streamed Install\nSuccess\n", nil
}

func unquote(s string) string {
	s = strings.TrimSuffix(strings.TrimPrefix(s, "'"), "'")
	return strings.ReplaceAll(s, `'\''`, "'")
}

func (d *demoBackend) dropFile(dir, name string) []string {
	var keep []string
	for _, e := range d.files[dir] {
		f := strings.Fields(e)
		n := strings.Join(f[1:], " ")
		if f[0] == "f" {
			n = strings.Join(f[1:len(f)-1], " ")
		}
		if n != name {
			keep = append(keep, e)
		}
	}
	return keep
}

func (d *demoBackend) ls(dir string) (string, error) {
	dir = strings.TrimSuffix(dir, "/")
	entries, ok := d.files[dir]
	if !ok {
		return "ls: " + dir + ": No such file or directory\n", errors.New("exit status 1")
	}
	var b strings.Builder
	b.WriteString("total 0\ndrwxrwx--x 4 root sdcard_rw 3488 2024-03-02 09:15 .\n")
	for _, e := range entries {
		f := strings.Fields(e)
		if f[0] == "d" {
			fmt.Fprintf(&b, "drwxrwx--x 2 root sdcard_rw 3488 2024-03-02 09:15 %s\n", strings.Join(f[1:], " "))
		} else {
			fmt.Fprintf(&b, "-rw-rw---- 1 root sdcard_rw %s 2024-05-18 21:40 %s\n", f[len(f)-1], strings.Join(f[1:len(f)-1], " "))
		}
	}
	return b.String(), nil
}

// remove handles the delete command: rmdir for empty folders, rm for files.
func (d *demoBackend) remove(cmd string) (string, error) {
	start := strings.Index(cmd, "'")
	end := strings.Index(cmd[start+1:], " ]")
	p := unquote(cmd[start : start+1+end])
	dir, name := path.Split(p)
	dir = strings.TrimSuffix(dir, "/")
	if kids, isDir := d.files[p]; isDir {
		if len(kids) > 0 {
			return "rmdir: '" + p + "': Directory not empty\n", errors.New("exit status 1")
		}
		delete(d.files, p)
	}
	d.files[dir] = d.dropFile(dir, name)
	return "deleted\n", nil
}

func (d *demoBackend) Reboot(serial string) error { return nil }
