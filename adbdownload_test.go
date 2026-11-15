package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeZip(t *testing.T, files map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "pt.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(0o755)
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	zw.Close()
	f.Close()
	return p
}

func TestUnzipPlatformTools(t *testing.T) {
	zp := writeZip(t, map[string]string{"platform-tools/adb": "bin", "platform-tools/NOTICE.txt": "n"})
	dir := t.TempDir()
	if err := unzipPlatformTools(zp, dir); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dir, "platform-tools", "adb"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode()&0o100 == 0 && os.PathSeparator == '/' {
		t.Errorf("adb isn't executable: %v", st.Mode())
	}
}

func TestUnzipRejectsEscapingPaths(t *testing.T) {
	zp := writeZip(t, map[string]string{"../evil": "x"})
	err := unzipPlatformTools(zp, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "bad path") {
		t.Fatalf("want bad path error, got %v", err)
	}
}
