package main

import (
	"archive/zip"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"
)

// axml builds a minimal binary AndroidManifest.xml: <manifest package="pkg">.
func axml(pkg string) []byte {
	le := binary.LittleEndian
	strs := []string{"manifest", "package", pkg}
	var data []byte
	var offs []uint32
	for _, s := range strs {
		offs = append(offs, uint32(len(data)))
		u := utf16.Encode([]rune(s))
		data = le.AppendUint16(data, uint16(len(u)))
		for _, c := range u {
			data = le.AppendUint16(data, c)
		}
		data = le.AppendUint16(data, 0)
	}
	for len(data)%4 != 0 {
		data = append(data, 0)
	}
	hdr := 28
	start := hdr + 4*len(strs)
	var pool []byte
	pool = le.AppendUint16(pool, 0x0001)
	pool = le.AppendUint16(pool, uint16(hdr))
	pool = le.AppendUint32(pool, uint32(start+len(data)))
	pool = le.AppendUint32(pool, uint32(len(strs)))
	pool = le.AppendUint32(pool, 0)
	pool = le.AppendUint32(pool, 0) // UTF-16
	pool = le.AppendUint32(pool, uint32(start))
	pool = le.AppendUint32(pool, 0)
	for _, o := range offs {
		pool = le.AppendUint32(pool, o)
	}
	pool = append(pool, data...)

	var el []byte
	el = le.AppendUint16(el, 0x0102)
	el = le.AppendUint16(el, 16)
	el = le.AppendUint32(el, 16+20+20)
	el = le.AppendUint32(el, 1)          // line
	el = le.AppendUint32(el, 0xffffffff) // comment
	el = le.AppendUint32(el, 0xffffffff) // ns
	el = le.AppendUint32(el, 0)          // name: manifest
	el = le.AppendUint16(el, 20)         // attribute start
	el = le.AppendUint16(el, 20)         // attribute size
	el = le.AppendUint16(el, 1)          // count
	el = le.AppendUint16(el, 0)
	el = le.AppendUint16(el, 0)
	el = le.AppendUint16(el, 0)
	el = le.AppendUint32(el, 0xffffffff) // attr ns
	el = le.AppendUint32(el, 1)          // attr name: package
	el = le.AppendUint32(el, 2)          // raw value
	el = le.AppendUint16(el, 8)
	el = append(el, 0, 0x03)
	el = le.AppendUint32(el, 2)

	var out []byte
	out = le.AppendUint16(out, 0x0003)
	out = le.AppendUint16(out, 8)
	out = le.AppendUint32(out, uint32(8+len(pool)+len(el)))
	return append(append(out, pool...), el...)
}

func TestManifestPackage(t *testing.T) {
	got, err := manifestPackage(axml("com.android.vending"))
	if err != nil || got != "com.android.vending" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := manifestPackage([]byte("not a manifest")); err == nil {
		t.Fatal("garbage should fail")
	}
}

func TestAPKPackageNameAndUnsigned(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.apk")
	f, _ := os.Create(p)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("AndroidManifest.xml")
	w.Write(axml("org.example.app"))
	zw.Close()
	f.Close()
	if got, err := apkPackageName(p); err != nil || got != "org.example.app" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := apkSignerCert(p); err == nil {
		t.Fatal("an unsigned APK should be rejected")
	}
}

func TestGooglePlayKeyRejectsOthers(t *testing.T) {
	if isGooglePlayKey(nil) || isGooglePlayKey([]byte("not a cert")) {
		t.Fatal("only Google's Play Store certificate should match")
	}
}
