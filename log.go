package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// activityLog keeps the last few hundred things the app did, in memory only,
// so people can copy them into a bug report. Nothing is written to disk.
type activityLog struct {
	mu    sync.Mutex
	lines []string
	max   int
	total int // lines ever added, so readers can ask for what's new
}

func newActivityLog() *activityLog { return &activityLog{max: 400} }

func (l *activityLog) add(format string, a ...any) {
	line := time.Now().Format("15:04:05") + "  " + fmt.Sprintf(format, a...)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, line)
	l.total++
	if len(l.lines) > l.max {
		l.lines = l.lines[len(l.lines)-l.max:]
	}
}

func (l *activityLog) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

// since returns the lines added after the first n, and the count to ask
// from next time. If n is ahead of the log (the app restarted), it starts over.
func (l *activityLog) since(n int) ([]string, int, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	reset := n < 0 || n > l.total
	first := l.total - len(l.lines) // number of the oldest line still kept
	if reset || n < first {
		n = first
	}
	out := append([]string{}, l.lines[n-first:]...) // [] not null in JSON
	return out, l.total, reset
}

// private hides the user's home folder and the tablet's full serial number,
// so the log can be pasted into a public bug report.
func private(s string, serials ...string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		s = strings.ReplaceAll(s, home, "~")
	}
	for _, sn := range serials {
		if len(sn) > 4 {
			s = strings.ReplaceAll(s, sn, "…"+sn[len(sn)-4:])
		}
	}
	return s
}

func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > max {
		s = s[:max] + "…"
	}
	return s
}

// loggedBackend records every command that changes the tablet, and its answer.
// Reads that run every few seconds (getprop, pm list) are left out.
type loggedBackend struct {
	Backend
	log *activityLog
}

func (b loggedBackend) Shell(serial string, args ...string) (string, error) {
	out, err := b.Backend.Shell(serial, args...)
	cmd := strings.Join(args, " ")
	if strings.HasPrefix(cmd, "getprop ") || strings.HasPrefix(cmd, "pm list packages") ||
		strings.HasPrefix(cmd, "ls -la ") || cmd == userAppsCmd {
		if err != nil {
			b.log.add("adb shell %s: error: %v", cmd, err)
		}
		return out, err
	}
	answer := oneLine(out, 200)
	if err != nil {
		answer = strings.TrimSpace(answer + " (error: " + err.Error() + ")")
	}
	b.log.add("adb shell %s -> %s", cmd, answer)
	return out, err
}

func (b loggedBackend) Push(serial, local, remote string) error {
	err := b.Backend.Push(serial, local, remote)
	if err != nil {
		b.log.add("adb push -> %s: error: %s", remote, oneLine(err.Error(), 200))
	} else {
		b.log.add("adb push -> %s: ok", remote)
	}
	return err
}

func (b loggedBackend) Pull(serial, remote, local string) error {
	err := b.Backend.Pull(serial, remote, local)
	if err != nil {
		b.log.add("adb pull %s: error: %s", remote, oneLine(err.Error(), 200))
	} else {
		b.log.add("adb pull %s: ok", remote)
	}
	return err
}

func (b loggedBackend) Install(serial, apk string) (string, error) {
	out, err := b.Backend.Install(serial, apk)
	answer := oneLine(out, 200)
	if err != nil {
		answer = strings.TrimSpace(answer + " (error: " + err.Error() + ")")
	}
	b.log.add("adb install -> %s", answer)
	return out, err
}

func (b loggedBackend) Reboot(serial string) error {
	err := b.Backend.Reboot(serial)
	if err != nil {
		b.log.add("adb reboot: error: %v", err)
	} else {
		b.log.add("adb reboot: ok")
	}
	return err
}

func (b loggedBackend) Pair(addr, code string) (string, error) {
	out, err := b.Backend.Pair(addr, code) // the code isn't logged
	b.logAnswer("adb pair "+addr, out, err)
	return out, err
}

func (b loggedBackend) Connect(addr string) (string, error) {
	out, err := b.Backend.Connect(addr)
	b.logAnswer("adb connect "+addr, out, err)
	return out, err
}

func (b loggedBackend) TCPIP(serial string, port int) (string, error) {
	out, err := b.Backend.TCPIP(serial, port)
	b.logAnswer(fmt.Sprintf("adb tcpip %d", port), out, err)
	return out, err
}

func (b loggedBackend) logAnswer(what, out string, err error) {
	answer := oneLine(out, 200)
	if err != nil {
		answer = strings.TrimSpace(answer + " (error: " + err.Error() + ")")
	}
	b.log.add("%s -> %s", what, answer)
}

// statusRecorder notes the status code and error text of API answers.
type statusRecorder struct {
	http.ResponseWriter
	code int
	body strings.Builder
}

func (r *statusRecorder) WriteHeader(code int) {
	r.code = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if r.code >= 400 && r.body.Len() < 600 {
		r.body.Write(p)
	}
	return r.ResponseWriter.Write(p)
}
