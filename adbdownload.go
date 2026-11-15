package main

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Android platform tools (adb) can be downloaded straight from Google into
// the app's own folder, so people don't need Homebrew, Scoop or admin rights.

const platformToolsBase = "https://dl.google.com/android/repository/platform-tools-latest-"

// The zip is about 7–15 MB; refuse anything absurdly large.
const maxPlatformToolsZip = 200 << 20

// platformToolsDir is where the downloaded copy lives.
func platformToolsDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "boox-debloat", "platform-tools")
}

func adbExe() string {
	if runtime.GOOS == "windows" {
		return "adb.exe"
	}
	return "adb"
}

// platformToolsURL returns Google's download for this computer, or "" when
// Google doesn't make one (for example Linux on ARM).
func platformToolsURL() string {
	switch runtime.GOOS {
	case "windows":
		return platformToolsBase + "windows.zip" // runs on ARM through emulation
	case "darwin":
		return platformToolsBase + "darwin.zip" // universal binary
	case "linux":
		if runtime.GOARCH == "amd64" {
			return platformToolsBase + "linux.zip"
		}
	}
	return ""
}

// canDownloadAdb reports whether the one-click download works here.
func canDownloadAdb() bool { return platformToolsURL() != "" }

// downloadAdb fetches platform tools from Google and unpacks them into
// platformToolsDir. It replaces an older downloaded copy only on success.
func downloadAdb(ctx context.Context) error {
	url := platformToolsURL()
	if url == "" {
		return errors.New("Google doesn't offer platform tools for this computer")
	}
	dest := platformToolsDir()
	parent := filepath.Dir(dest)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}

	zipFile, err := os.CreateTemp(parent, "platform-tools-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(zipFile.Name())
	defer zipFile.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach Google: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Google's server answered %s", resp.Status)
	}
	n, err := io.Copy(zipFile, io.LimitReader(resp.Body, maxPlatformToolsZip+1))
	if err != nil {
		return fmt.Errorf("the download stopped: %w", err)
	}
	if n > maxPlatformToolsZip {
		return errors.New("the download is unexpectedly large")
	}

	staging, err := os.MkdirTemp(parent, "platform-tools-new-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := unzipPlatformTools(zipFile.Name(), staging); err != nil {
		return err
	}
	// The zip holds a single platform-tools folder.
	got := filepath.Join(staging, "platform-tools")
	if st, err := os.Stat(filepath.Join(got, adbExe())); err != nil || st.IsDir() {
		return errors.New("the download didn't contain adb")
	}

	old := dest + ".old"
	os.RemoveAll(old)
	if _, err := os.Stat(dest); err == nil {
		if err := os.Rename(dest, old); err != nil {
			return err
		}
	}
	if err := os.Rename(got, dest); err != nil {
		os.Rename(old, dest)
		return err
	}
	os.RemoveAll(old)
	return nil
}

// unzipPlatformTools extracts the zip into dir, refusing paths that would
// land outside it.
func unzipPlatformTools(zipPath, dir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("the download is damaged: %w", err)
	}
	defer r.Close()
	root := filepath.Clean(dir) + string(os.PathSeparator)
	for _, f := range r.File {
		target := filepath.Join(dir, f.Name)
		if !strings.HasPrefix(target, root) {
			return fmt.Errorf("the download has a bad path: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if f.Mode()&os.ModeSymlink != 0 {
			continue // platform tools don't need links
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if f.Mode()&0o111 != 0 || strings.HasSuffix(strings.ToLower(f.Name), ".exe") {
			mode = 0o755
		}
		if err := extractFile(f, target, mode); err != nil {
			return err
		}
	}
	return nil
}

func extractFile(f *zip.File, target string, mode os.FileMode) error {
	src, err := f.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, io.LimitReader(src, maxPlatformToolsZip)); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}

// downloadTimeout is generous for slow connections.
const downloadTimeout = 10 * time.Minute
