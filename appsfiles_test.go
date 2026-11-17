package main

import (
	"errors"
	"strings"
	"testing"
)

func TestCleanPath(t *testing.T) {
	ok := map[string]string{
		"/sdcard":                   "/sdcard",
		"/sdcard/Books/":            "/sdcard/Books",
		"/sdcard/Books/../Download": "/sdcard/Download",
		"sdcard/My Books":           "/sdcard/My Books",
	}
	for in, want := range ok {
		got, err := cleanPath(in)
		if err != nil || got != want {
			t.Errorf("cleanPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"/", "/system", "/sdcard/../data", "/sdcardx", "/sdcard/a\nb", "/storage/emulated/0"} {
		if _, err := cleanPath(bad); err == nil {
			t.Errorf("cleanPath(%q) should fail", bad)
		}
	}
}

func TestCanDelete(t *testing.T) {
	for p, want := range map[string]bool{
		"/sdcard":                false,
		"/sdcard/Android":        false,
		"/sdcard/Android/data/x": false,
		"/sdcard/Books/a.epub":   true,
		"/sdcard/AndroidBooks":   true,
	} {
		if canDelete(p) != want {
			t.Errorf("canDelete(%q) = %v", p, !want)
		}
	}
}

func TestQuote(t *testing.T) {
	if got := quote("it's; rm -rf /"); got != `'it'\''s; rm -rf /'` {
		t.Fatalf("quote: %s", got)
	}
}

func TestSafeFileName(t *testing.T) {
	for in, want := range map[string]string{"book.epub": "book.epub", "../../x.txt": "x.txt", `C:\Users\a\b.pdf`: "b.pdf"} {
		if got, err := safeFileName(in); err != nil || got != want {
			t.Errorf("safeFileName(%q) = %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "..", "a\nb"} {
		if _, err := safeFileName(bad); err == nil {
			t.Errorf("safeFileName(%q) should fail", bad)
		}
	}
}

func TestParseLs(t *testing.T) {
	out := `total 24
drwxrwx--x  4 root sdcard_rw 3488 2024-03-02 09:15 .
drwx--x--x  4 root sdcard_rw 3488 2024-03-02 09:15 ..
-rw-rw----  1 root sdcard_rw 512340 2024-05-18 21:40 Pride and Prejudice.epub
drwxrwx--x  2 root sdcard_rw 3488 2024-03-02 09:15 Manga
lrwxrwxrwx  1 root root 21 2024-03-02 09:15 link -> /storage/self/primary
`
	got := parseLs(out)
	if len(got) != 3 || !got[0].Dir || got[0].Name != "Manga" {
		t.Fatalf("unexpected: %+v", got)
	}
	if got[2].Name != "Pride and Prejudice.epub" || got[2].Size != 512340 {
		t.Fatalf("file parsed wrong: %+v", got[2])
	}
	if got[1].Name != "link" {
		t.Fatalf("link parsed wrong: %+v", got[1])
	}
}

func TestInstallResult(t *testing.T) {
	if ok, _ := installResult("Performing Streamed Install\nSuccess\n", nil); !ok {
		t.Error("success not detected")
	}
	ok, msg := installResult("adb: failed to install x.apk: Failure [INSTALL_FAILED_OLDER_SDK: Requires newer sdk version]", errors.New("exit status 1"))
	if ok || !strings.Contains(msg, "newer version of Android") {
		t.Errorf("got %v %q", ok, msg)
	}
	ok, msg = installResult("Failure [INSTALL_FAILED_SOMETHING_NEW]", nil)
	if ok || !strings.Contains(msg, "INSTALL_FAILED_SOMETHING_NEW") {
		t.Errorf("got %v %q", ok, msg)
	}
}

func TestParseUserApps(t *testing.T) {
	got := parseUserApps("org.koreader.launcher.fdroid versionName=2025.10\ncom.aurora.store versionName=4.8.4\n\nnot a package\n")
	if len(got) != 2 || got[0].ID != "com.aurora.store" || got[0].Version != "4.8.4" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestRecommendedLoads(t *testing.T) {
	g, err := loadRecommended()
	if err != nil || len(g) == 0 {
		t.Fatal(err)
	}
	for _, grp := range g {
		for _, a := range grp.Apps {
			if a.Name == "" || len(a.IDs) == 0 || !strings.HasPrefix(a.URL, "https://") {
				t.Errorf("bad entry: %+v", a)
			}
			if strings.Contains(a.URL, "apkpure") {
				t.Errorf("%s links to APKPure", a.Name)
			}
		}
	}
}

func TestDemoFiles(t *testing.T) {
	d := newDemoBackend(testCatalog(t))
	out, _ := d.Shell("x", "ls -la "+quote("/sdcard/Books/"))
	if len(parseLs(out)) != 3 {
		t.Fatalf("demo ls: %q", out)
	}
	cmd := "if [ -d " + quote("/sdcard/Books/Manga") + " ]; then rmdir -- x; fi && echo deleted"
	if out, _ := d.Shell("x", cmd); !strings.Contains(out, "deleted") {
		t.Fatalf("delete empty folder: %q", out)
	}
	cmd = "if [ -d " + quote("/sdcard/Books") + " ]; then rmdir -- x; fi && echo deleted"
	if out, _ := d.Shell("x", cmd); strings.Contains(out, "deleted") {
		t.Fatal("should refuse a non-empty folder")
	}
}

func TestSafeRelPath(t *testing.T) {
	good := map[string]string{
		"a.txt":                      "a.txt",
		"simpleui.koplugin/main.lua": "simpleui.koplugin/main.lua",
		"Folder//sub/x.png":          "Folder/sub/x.png",
		`Win\sub\file.txt`:           "Win/sub/file.txt",
	}
	for in, want := range good {
		got, err := safeRelPath(in)
		if err != nil || got != want {
			t.Errorf("safeRelPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "/", "../x", "a/../../b", "a/./b", "a\nb"} {
		if got, err := safeRelPath(in); err == nil {
			t.Errorf("safeRelPath(%q) = %q, want error", in, got)
		}
	}
}
