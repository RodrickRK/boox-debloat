package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Recommended apps
// ---------------------------------------------------------------------------

//go:embed recommended.json
var recommendedJSON []byte

type recApp struct {
	Name      string   `json:"name"`
	IDs       []string `json:"ids"`
	Why       string   `json:"why"`
	Source    string   `json:"source"`
	URL       string   `json:"url"`
	Installed bool     `json:"installed"`
}

type recGroup struct {
	Group string   `json:"group"`
	Apps  []recApp `json:"apps"`
}

func loadRecommended() ([]recGroup, error) {
	var g []recGroup
	if err := json.Unmarshal(recommendedJSON, &g); err != nil {
		return nil, fmt.Errorf("reading recommended.json: %w", err)
	}
	return g, nil
}

// ---------------------------------------------------------------------------
// User apps
// ---------------------------------------------------------------------------

type userApp struct {
	ID      string `json:"id"`
	Name    string `json:"name,omitempty"` // known name from the recommended list
	Version string `json:"version,omitempty"`
}

// The tablet prints one line per app: "<package> versionName=<version>".
const userAppsCmd = `for p in $(pm list packages -3 | cut -d: -f2); do echo "$p $(dumpsys package $p | grep -m1 versionName)"; done`

func parseUserApps(out string) []userApp {
	list := []userApp{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || !validPackage(f[0]) {
			continue
		}
		a := userApp{ID: f[0]}
		for _, x := range f[1:] {
			if v, ok := strings.CutPrefix(x, "versionName="); ok {
				a.Version = v
			}
		}
		list = append(list, a)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return list
}

var packageRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z0-9_]+)+$`)

func validPackage(id string) bool { return packageRe.MatchString(id) }

type appsInstalledResp struct {
	Apps        []userApp  `json:"apps"`
	Recommended []recGroup `json:"recommended"`
	Abi         string     `json:"abi"`
}

func (s *Server) userApps(serial string) ([]userApp, error) {
	out, err := s.be.Shell(serial, userAppsCmd)
	if err != nil {
		return nil, fmt.Errorf("reading your apps: %w", err)
	}
	return parseUserApps(out), nil
}

func (s *Server) handleUserApps(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dev, ok := s.readyDevice(w, false)
	if !ok {
		return
	}
	apps, err := s.userApps(dev.Serial)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	rec, err := loadRecommended()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	have := map[string]bool{}
	for _, a := range apps {
		have[a.ID] = true
	}
	names := map[string]string{}
	for gi := range rec {
		for ai := range rec[gi].Apps {
			ra := &rec[gi].Apps[ai]
			for _, id := range ra.IDs {
				names[id] = ra.Name
				if have[id] {
					ra.Installed = true
				}
			}
		}
	}
	for i := range apps {
		apps[i].Name = names[apps[i].ID]
	}
	writeJSON(w, http.StatusOK, appsInstalledResp{Apps: apps, Recommended: rec, Abi: s.prop(dev.Serial, "ro.product.cpu.abi")})
}

// installMessages turns adb's failure codes into plain words.
var installMessages = map[string]string{
	"INSTALL_FAILED_UPDATE_INCOMPATIBLE":        "A version of this app signed by a different developer is already installed. Uninstall it first, then try again.",
	"INSTALL_FAILED_VERSION_DOWNGRADE":          "A newer version of this app is already installed.",
	"INSTALL_FAILED_OLDER_SDK":                  "This app needs a newer version of Android than the tablet has (Android 11).",
	"INSTALL_FAILED_NO_MATCHING_ABIS":           "This APK is built for a different kind of processor. Download the arm64-v8a, armeabi-v7a or universal APK instead.",
	"INSTALL_FAILED_INSUFFICIENT_STORAGE":       "The tablet doesn't have enough free space.",
	"INSTALL_PARSE_FAILED_NOT_APK":              "That file isn't an Android app.",
	"INSTALL_PARSE_FAILED_NO_CERTIFICATES":      "The APK isn't signed, so Android won't install it.",
	"INSTALL_FAILED_INVALID_APK":                "The APK is damaged or incomplete. Download it again.",
	"INSTALL_FAILED_USER_RESTRICTED":            "The tablet blocked the install. Allow installs over USB on the tablet and try again.",
	"INSTALL_FAILED_ALREADY_EXISTS":             "This app is already installed.",
	"INSTALL_FAILED_DUPLICATE_PERMISSION":       "Another installed app uses the same permission name. Uninstall it first.",
	"INSTALL_PARSE_FAILED_UNEXPECTED_EXCEPTION": "The APK couldn't be read. Download it again.",
}

var failureRe = regexp.MustCompile(`Failure \[([A-Z_]+)`)

// installResult reports whether adb install worked, and why not in plain words.
func installResult(out string, err error) (bool, string) {
	if strings.Contains(out, "Success") {
		return true, ""
	}
	if m := failureRe.FindStringSubmatch(out); m != nil {
		if msg, ok := installMessages[m[1]]; ok {
			return false, msg
		}
		return false, "The install didn't work: " + m[1] + "."
	}
	if err != nil {
		if t := strings.TrimSpace(out); t != "" {
			return false, "The install didn't work: " + oneLine(t, 200)
		}
		return false, "The install didn't work: " + err.Error()
	}
	return false, "The install didn't work: " + oneLine(out, 200)
}

const maxAPKBytes = 600 << 20

func (s *Server) handleInstall(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAPKBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "That file is too big, or the upload stopped.")
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, hdr, err := r.FormFile("apk")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "No APK was sent.")
		return
	}
	defer file.Close()
	if !strings.EqualFold(filepath.Ext(hdr.Filename), ".apk") {
		writeErr(w, http.StatusBadRequest, "Choose an .apk file. Split APK bundles (.apks, .xapk) aren't supported.")
		return
	}
	tmp, err := saveTemp(file, "boox-app-*.apk")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer os.Remove(tmp)

	s.mu.Lock()
	defer s.mu.Unlock()
	dev, ok := s.readyDevice(w, true)
	if !ok {
		return
	}
	s.log.add("Installing %s", hdr.Filename)
	okInstall, msg := installResult(s.be.Install(dev.Serial, tmp))
	if !okInstall {
		writeErr(w, http.StatusBadGateway, msg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": hdr.Filename})
}

func (s *Server) handleUninstall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil || !validPackage(body.ID) {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dev, ok := s.readyDevice(w, true)
	if !ok {
		return
	}
	// Only apps you installed yourself. System apps belong to Debloat.
	mine, err := listPackages(s.be, dev.Serial, "-3")
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if !mine[body.ID] {
		writeErr(w, http.StatusBadRequest, body.ID+" isn't an app you installed, so it can't be uninstalled here.")
		return
	}
	out, err := s.be.Shell(dev.Serial, "pm", "uninstall", body.ID)
	if err != nil || !strings.Contains(out, "Success") {
		writeErr(w, http.StatusBadGateway, "Couldn't uninstall: "+oneLine(strings.TrimSpace(out+" "+errText(err)), 200))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------------------------------------------------------------------------
// Files
// ---------------------------------------------------------------------------

const storageRoot = "/sdcard"

// cleanPath keeps every file action inside the tablet's shared storage.
func cleanPath(p string) (string, error) {
	if strings.ContainsAny(p, "\x00\n\r") {
		return "", errors.New("That path has characters the tablet can't use.")
	}
	c := path.Clean("/" + strings.TrimSpace(p))
	if c != storageRoot && !strings.HasPrefix(c, storageRoot+"/") {
		return "", errors.New("Only files in the tablet's storage (/sdcard) can be used.")
	}
	return c, nil
}

// canDelete refuses the storage root and the Android folder, where apps keep
// their data.
func canDelete(p string) bool {
	return p != storageRoot && p != storageRoot+"/Android" && !strings.HasPrefix(p, storageRoot+"/Android/")
}

// quote makes a value safe to pass through the tablet's shell.
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// safeFileName keeps the name people gave a file, minus anything that could
// point somewhere else.
func safeFileName(name string) (string, error) {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == ".." || name == "/" || name == "" || strings.ContainsAny(name, "\x00\n\r/") {
		return "", errors.New("that file name can't be used")
	}
	return name, nil
}

type fileEntry struct {
	Name     string `json:"name"`
	Dir      bool   `json:"dir"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
}

// One line of `ls -la` from toybox, for example:
// drwxrwx--x 2 root sdcard_rw 3488 2023-07-20 17:44 Books
var lsRe = regexp.MustCompile(`^([dl\-])\S*\s+\d+\s+\S+\s+\S+\s+(\d+)\s+(\d{4}-\d{2}-\d{2} \d{2}:\d{2})\s(.+)$`)

func parseLs(out string) []fileEntry {
	list := []fileEntry{}
	for _, line := range strings.Split(out, "\n") {
		m := lsRe.FindStringSubmatch(strings.TrimRight(line, " "))
		if m == nil {
			continue
		}
		name := m[4]
		if m[1] == "l" {
			name, _, _ = strings.Cut(name, " -> ")
		}
		if name == "." || name == ".." {
			continue
		}
		size, _ := strconv.ParseInt(m[2], 10, 64)
		e := fileEntry{Name: name, Dir: m[1] == "d", Size: size, Modified: m[3]}
		if e.Dir {
			e.Size = 0
		}
		list = append(list, e)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Dir != list[j].Dir {
			return list[i].Dir
		}
		return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
	})
	return list
}

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	p, err := cleanPath(r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dev, ok := s.readyDevice(w, false)
	if !ok {
		return
	}
	out, err := s.be.Shell(dev.Serial, "ls -la "+quote(p+"/"))
	if err != nil && !lsRe.MatchString(out) {
		writeErr(w, http.StatusBadGateway, "Couldn't open that folder: "+oneLine(strings.TrimSpace(out+" "+errText(err)), 200))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": p, "entries": parseLs(out)})
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	p, err := cleanPath(r.URL.Query().Get("path"))
	if err != nil || p == storageRoot {
		writeErr(w, http.StatusBadRequest, "Choose a file to download.")
		return
	}
	tmpDir, err := os.MkdirTemp("", "boox-pull-*")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer os.RemoveAll(tmpDir)
	local := filepath.Join(tmpDir, "file")

	s.mu.Lock()
	dev, ok := s.readyDevice(w, false)
	if !ok {
		s.mu.Unlock()
		return
	}
	err = s.be.Pull(dev.Serial, p, local)
	s.mu.Unlock()
	if err != nil {
		writeErr(w, http.StatusBadGateway, "Couldn't copy the file from the tablet: "+oneLine(err.Error(), 200))
		return
	}
	f, err := os.Open(local)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer f.Close()
	name := path.Base(p)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(name))
	w.Header().Set("Cache-Control", "no-store")
	io.Copy(w, f)
}

const maxUploadBytes = 2 << 30

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "Those files are too big (2 GB at most), or the upload stopped.")
		return
	}
	defer r.MultipartForm.RemoveAll()
	dir, err := cleanPath(r.FormValue("path"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		writeErr(w, http.StatusBadRequest, "No files were sent.")
		return
	}
	type saved struct{ local, name string }
	var list []saved
	defer func() {
		for _, f := range list {
			os.Remove(f.local)
		}
	}()
	for _, fh := range files {
		name, err := safeFileName(fh.Filename)
		if err != nil {
			writeErr(w, http.StatusBadRequest, fmt.Sprintf("%q: %v", fh.Filename, err))
			return
		}
		local, err := saveUpload(fh)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		list = append(list, saved{local, name})
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	dev, ok := s.readyDevice(w, true)
	if !ok {
		return
	}
	var done []string
	for _, f := range list {
		remote := dir + "/" + f.name
		if err := s.be.Push(dev.Serial, f.local, remote); err != nil {
			writeErr(w, http.StatusBadGateway, fmt.Sprintf("Couldn't copy %s to the tablet: %s", f.name, oneLine(err.Error(), 200)))
			return
		}
		// Index it so book and gallery apps see it straight away.
		s.be.Shell(dev.Serial, "am broadcast -a android.intent.action.MEDIA_SCANNER_SCAN_FILE -d "+quote("file://"+remote))
		done = append(done, f.name)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "files": done})
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	p, err := cleanPath(body.Path)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if !canDelete(p) {
		writeErr(w, http.StatusBadRequest, "This tool doesn't delete the storage folder itself or anything in the Android folder, where apps keep their data.")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dev, ok := s.readyDevice(w, true)
	if !ok {
		return
	}
	// Files, and folders only when they're empty: nothing is deleted in bulk.
	cmd := "if [ -d " + quote(p) + " ]; then rmdir -- " + quote(p) + "; else rm -- " + quote(p) + "; fi && echo deleted"
	out, err := s.be.Shell(dev.Serial, cmd)
	if !strings.Contains(out, "deleted") {
		msg := oneLine(strings.TrimSpace(out+" "+errText(err)), 200)
		if strings.Contains(strings.ToLower(msg), "not empty") {
			msg = "That folder isn't empty. Delete the files in it first."
		} else {
			msg = "Couldn't delete it: " + msg
		}
		writeErr(w, http.StatusBadGateway, msg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func saveTemp(src io.Reader, pattern string) (string, error) {
	tmp, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), tmp.Close()
}

func saveUpload(fh *multipart.FileHeader) (string, error) {
	f, err := fh.Open()
	if err != nil {
		return "", err
	}
	defer f.Close()
	return saveTemp(f, "boox-upload-*")
}
