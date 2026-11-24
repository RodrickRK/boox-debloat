package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// App states as shown in the UI.
const (
	stateEnabled  = "enabled"
	stateDisabled = "disabled"
	stateRemoved  = "removed" // uninstalled for user 0, can be restored
	stateMissing  = "missing" // not part of this firmware
)

// Actions a change can ask for.
const (
	actRemove  = "remove"
	actRestore = "restore"
	actDisable = "disable"
	actEnable  = "enable"
	// actRemoveKeep removes for user 0 but keeps the app's data, so the
	// GSF ID survives if Google comes back. Only De-Google uses it; the
	// API never accepts it from the page.
	actRemoveKeep = "remove-keep-data"
)

// Change is one requested action on one app.
type Change struct {
	ID     string `json:"id"`
	Action string `json:"action"`
}

// Result reports how one change went.
type Result struct {
	ID      string `json:"id"`
	Action  string `json:"action"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

func listPackages(b Backend, serial string, flags ...string) (map[string]bool, error) {
	args := append([]string{"pm", "list", "packages"}, flags...)
	out, err := b.Shell(serial, args...)
	if err != nil {
		return nil, fmt.Errorf("reading the app list: %w", err)
	}
	set := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if p, ok := strings.CutPrefix(line, "package:"); ok && p != "" {
			set[p] = true
		}
	}
	return set, nil
}

// readStates returns the state of every package the catalog knows about.
func readStates(b Backend, serial string, c *Catalog) (map[string]string, error) {
	enabled, err := listPackages(b, serial, "--user", "0", "-e")
	if err != nil {
		return nil, err
	}
	disabled, err := listPackages(b, serial, "--user", "0", "-d")
	if err != nil {
		return nil, err
	}
	everything, err := listPackages(b, serial, "-u")
	if err != nil {
		return nil, err
	}
	// Apps installed after the firmware (like Play Store here) lose their
	// APK when removed: Android keeps only a record and the data, and
	// install-existing can't bring them back. Count those as missing.
	added, err := listPackages(b, serial, "-u", "-3")
	if err != nil {
		return nil, err
	}
	states := map[string]string{}
	for _, a := range c.all() {
		switch {
		case enabled[a.ID]:
			states[a.ID] = stateEnabled
		case disabled[a.ID]:
			states[a.ID] = stateDisabled
		case everything[a.ID] && !added[a.ID]:
			states[a.ID] = stateRemoved
		default:
			states[a.ID] = stateMissing
		}
	}
	return states, nil
}

// playPackages are what Boox's "Enable Google Play" switch turns on.
var playPackages = []string{"com.google.android.gsf", "com.google.android.gms", "com.android.vending"}

// playChanges returns what's needed to turn Google Play on: restore what
// was removed and enable what was turned off. Missing packages are skipped.
func playChanges(states map[string]string) []Change {
	var out []Change
	for _, id := range playPackages {
		switch states[id] {
		case stateRemoved:
			out = append(out, Change{ID: id, Action: actRestore})
		case stateDisabled:
			out = append(out, Change{ID: id, Action: actEnable})
		}
	}
	return out
}

// validate checks every change before anything is sent to the tablet.
// Protected apps can only ever be restored or enabled.
func validate(c *Catalog, changes []Change) error {
	if len(changes) == 0 {
		return fmt.Errorf("no changes to apply")
	}
	for _, ch := range changes {
		g := c.group(ch.ID)
		if g == "" {
			return fmt.Errorf("%s isn't on this tool's list, so it can't be changed here", ch.ID)
		}
		switch ch.Action {
		case actRestore, actEnable:
			// Allowed for every listed app.
		case actRemove:
			if g == groupGoogle {
				return fmt.Errorf("%s is removed with De-Google, together with the rest of Google", ch.ID)
			}
			if g != groupRemove {
				return fmt.Errorf("%s can't be removed with this tool", ch.ID)
			}
		case actDisable:
			if g != groupDisable {
				return fmt.Errorf("%s can't be turned off with this tool", ch.ID)
			}
		default:
			return fmt.Errorf("unknown action %q", ch.Action)
		}
	}
	return nil
}

// applyChange runs one change and checks adb's answer. It refuses to remove
// or turn off anything the tablet needs, whatever the caller asked.
func applyChange(b Backend, c *Catalog, serial string, ch Change) Result {
	switch ch.Action {
	case actRemove, actRemoveKeep, actDisable:
		if c.kept(ch.ID) {
			return Result{ID: ch.ID, Action: ch.Action, Message: "The tablet needs this app, so it's always kept."}
		}
	}
	var args []string
	var want string
	switch ch.Action {
	case actRemove:
		args, want = []string{"pm", "uninstall", "--user", "0", ch.ID}, "Success"
	case actRemoveKeep:
		args, want = []string{"pm", "uninstall", "-k", "--user", "0", ch.ID}, "Success"
	case actRestore:
		args, want = []string{"cmd", "package", "install-existing", ch.ID}, "installed for user"
	case actDisable:
		args, want = []string{"pm", "disable-user", "--user", "0", ch.ID}, "disabled"
	case actEnable:
		args, want = []string{"pm", "enable", "--user", "0", ch.ID}, "enabled"
	}
	out, err := b.Shell(serial, args...)
	out = strings.TrimSpace(out)
	r := Result{ID: ch.ID, Action: ch.Action, Message: out}
	if err == nil && strings.Contains(out, want) {
		r.OK = true
	} else if err != nil && out == "" {
		r.Message = err.Error()
	}
	// install-existing says "installed" even when only a record is left
	// and the APK is gone. Check the app really has its files.
	if r.OK && ch.Action == actRestore {
		if p, _ := b.Shell(serial, "pm", "path", ch.ID); !strings.Contains(p, "package:") {
			r.OK = false
			r.Message = "This app was installed after the firmware, so removing it deleted it. Install it again from its APK."
		}
	}
	return r
}

// Backup is a snapshot of app states saved before every change.
type Backup struct {
	Time     time.Time         `json:"time"`
	Serial   string            `json:"serial"`
	Model    string            `json:"model"`
	Firmware string            `json:"firmware"`
	States   map[string]string `json:"states"`
}

func backupDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "boox-debloat", "backups")
}

func saveBackup(bk Backup) (string, error) {
	dir := backupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s-%s.json", safeName(bk.Serial), bk.Time.Format("20060102-150405"))
	path := filepath.Join(dir, name)
	data, err := json.MarshalIndent(bk, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, data, 0o644)
}

// safeName keeps letters, digits, dash and underscore.
func safeName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ' || r == '.':
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "device"
	}
	return out
}
