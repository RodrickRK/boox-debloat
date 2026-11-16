package main

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Some firmware (like the C69 M Star's Star Education build) ships Google
// Play services and Google Services Framework but no Play Store. Google
// doesn't offer the Play Store as a download, so the person picks the APK
// themselves and the app checks it before installing:
//   - its manifest must say it's com.android.vending, and
//   - it must be signed with Google's Play Store key, or with the same key
//     as the Google apps that came with the tablet. (The C69 M Star's own
//     Google apps use a different Google key from the Play Store.)

const playStorePkg = "com.android.vending"

// googlePlayKeySHA256 is the SHA-256 of the certificate Google signs the
// Play Store and Play services with (SHA-1 38:91:8A:45:...:2C:ED:57:88).
const googlePlayKeySHA256 = "F0FD6C5B410F25CB25C3B53346C8972FAE30F8EE7411DF910480AD6B2D60DB83"

func isGooglePlayKey(cert []byte) bool {
	return len(cert) > 0 && fmt.Sprintf("%X", sha256.Sum256(cert)) == googlePlayKeySHA256
}

// googleRefPackages are compared against, smallest first, since each one is
// copied off the tablet to read its certificate.
var googleRefPackages = []string{"com.google.android.gsf", "com.google.android.gms"}

// tabletCert copies a package's APK off the tablet and returns its signing
// certificate.
func (s *Server) tabletCert(serial, pkg string) ([]byte, error) {
	out, err := s.be.Shell(serial, "pm", "path", pkg)
	if err != nil {
		return nil, err
	}
	var apk string
	for _, line := range strings.Split(out, "\n") {
		if p, ok := strings.CutPrefix(strings.TrimSpace(line), "package:"); ok && strings.HasSuffix(p, ".apk") {
			apk = p
			break
		}
	}
	if apk == "" {
		return nil, fmt.Errorf("%s isn't on the tablet", pkg)
	}
	dir, err := os.MkdirTemp("", "boox-ref-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	local := filepath.Join(dir, "ref.apk")
	if err := s.be.Pull(serial, apk, local); err != nil {
		return nil, err
	}
	return apkSignerCert(local)
}

// checkGoogleSigned compares cert with the tablet's own Google apps.
// Caller holds s.mu.
func (s *Server) checkGoogleSigned(serial string, cert []byte) error {
	if isGooglePlayKey(cert) {
		s.log.add("Play Store file is signed with Google's Play Store key (%s)", certFingerprint(cert))
		return nil
	}
	var tried []string
	for _, pkg := range googleRefPackages {
		ref, err := s.tabletCert(serial, pkg)
		if err != nil {
			s.log.add("Couldn't read %s's certificate: %v", pkg, err)
			continue
		}
		if sameCert(cert, ref) {
			s.log.add("Play Store file is signed with the same key as %s (%s)", pkg, certFingerprint(cert))
			return nil
		}
		tried = append(tried, certFingerprint(ref))
	}
	s.log.add("Play Store file key %s isn't Google's Play Store key or the tablet's Google key (%s)", certFingerprint(cert), strings.Join(tried, ", "))
	return fmt.Errorf("this file isn't signed by Google, so it wasn't installed. Download the Play Store again from a source you trust")
}

func (s *Server) handleInstallPlay(w http.ResponseWriter, r *http.Request) {
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
		writeErr(w, http.StatusBadRequest, "Choose the Play Store .apk file. Split bundles (.apks, .xapk, .apkm) aren't supported.")
		return
	}
	tmp, err := saveTemp(file, "boox-play-*.apk")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer os.Remove(tmp)

	pkg, err := apkPackageName(tmp)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "Couldn't read that file: "+err.Error()+".")
		return
	}
	if pkg != playStorePkg {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("That file is %s, not Google Play Store (%s).", pkg, playStorePkg))
		return
	}
	cert, err := apkSignerCert(tmp)
	if err != nil && !s.demo {
		writeErr(w, http.StatusBadRequest, "Couldn't check who signed that file: "+err.Error()+".")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	dev, ok := s.readyDevice(w, true)
	if !ok {
		return
	}
	s.log.add("Checking Play Store file %s", hdr.Filename)
	if s.demo {
		s.log.add("Demo mode: skipping the signature check")
	} else if err := s.checkGoogleSigned(dev.Serial, cert); err != nil {
		writeErr(w, http.StatusBadRequest, strings.ToUpper(err.Error()[:1])+err.Error()[1:]+".")
		return
	}
	s.log.add("Installing Play Store")
	okInstall, msg := installResult(s.be.Install(dev.Serial, tmp))
	if !okInstall {
		writeErr(w, http.StatusBadGateway, msg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
