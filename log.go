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
}

func newActivityLog() *activityLog { return &activityLog{max: 400} }

func (l *activityLog) add(format string, a ...any) {
	line := time.Now().Format("15:04:05") + "  " + fmt.Sprintf(format, a...)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, line)
	if len(l.lines) > l.max {
		l.lines = l.lines[len(l.lines)-l.max:]
	}
}

func (l *activityLog) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
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
	if strings.HasPrefix(cmd, "getprop ") || strings.HasPrefix(cmd, "pm list packages") {
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

func (b loggedBackend) Reboot(serial string) error {
	err := b.Backend.Reboot(serial)
	if err != nil {
		b.log.add("adb reboot: error: %v", err)
	} else {
		b.log.add("adb reboot: ok")
	}
	return err
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
