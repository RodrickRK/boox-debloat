package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf16"
)

// Reading two facts from an APK without Android tools: the package name in
// its manifest, and the certificate it was signed with. They're used to make
// sure a Play Store file really is Google's before it's installed.

var errNoSignature = errors.New("the APK isn't signed")

// apkSignerCert returns the DER bytes of the APK's first signing certificate.
// It reads the v3 or v2 signature block, and falls back to a v1 signature.
func apkSignerCert(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if cert, err := signingBlockCert(f, st.Size()); err == nil {
		return cert, nil
	}
	return v1Cert(path)
}

// signingBlockCert reads the APK Signing Block that sits just before the
// zip's central directory (APK Signature Scheme v2 and v3).
func signingBlockCert(r io.ReaderAt, size int64) ([]byte, error) {
	cdOffset, err := centralDirOffset(r, size)
	if err != nil {
		return nil, err
	}
	if cdOffset < 32 {
		return nil, errNoSignature
	}
	footer := make([]byte, 24)
	if _, err := r.ReadAt(footer, cdOffset-24); err != nil {
		return nil, err
	}
	if string(footer[8:]) != "APK Sig Block 42" {
		return nil, errNoSignature
	}
	blockSize := int64(binary.LittleEndian.Uint64(footer[:8]))
	start := cdOffset - blockSize - 8
	if blockSize < 24 || blockSize > 64<<20 || start < 0 {
		return nil, errors.New("the APK's signature block is damaged")
	}
	block := make([]byte, blockSize-24) // the id-value pairs
	if _, err := r.ReadAt(block, start+8); err != nil {
		return nil, err
	}
	pairs := map[uint32][]byte{}
	for len(block) >= 12 {
		n := binary.LittleEndian.Uint64(block[:8])
		if n < 4 || n > uint64(len(block)-8) {
			return nil, errors.New("the APK's signature block is damaged")
		}
		pairs[binary.LittleEndian.Uint32(block[8:12])] = block[12 : 8+n]
		block = block[8+n:]
	}
	for _, id := range []uint32{0xf05368c0, 0x7109871a} { // v3, v2
		if v, ok := pairs[id]; ok {
			return firstSchemeCert(v)
		}
	}
	return nil, errNoSignature
}

// centralDirOffset finds the zip's end-of-central-directory record.
func centralDirOffset(r io.ReaderAt, size int64) (int64, error) {
	tail := int64(22 + 65535)
	if tail > size {
		tail = size
	}
	buf := make([]byte, tail)
	if _, err := r.ReadAt(buf, size-tail); err != nil && err != io.EOF {
		return 0, err
	}
	for i := len(buf) - 22; i >= 0; i-- {
		if binary.LittleEndian.Uint32(buf[i:]) == 0x06054b50 {
			return int64(binary.LittleEndian.Uint32(buf[i+16:])), nil
		}
	}
	return 0, errors.New("that file isn't an APK")
}

// lp reads one uint32-length-prefixed field.
func lp(b []byte) (field, rest []byte, err error) {
	if len(b) < 4 {
		return nil, nil, errors.New("short field")
	}
	n := binary.LittleEndian.Uint32(b)
	if uint64(n) > uint64(len(b)-4) {
		return nil, nil, errors.New("field too long")
	}
	return b[4 : 4+n], b[4+n:], nil
}

// firstSchemeCert walks signers -> signer -> signed data -> certificates.
func firstSchemeCert(v []byte) ([]byte, error) {
	bad := errors.New("the APK's signature is damaged")
	signers, _, err := lp(v)
	if err != nil {
		return nil, bad
	}
	signer, _, err := lp(signers)
	if err != nil {
		return nil, bad
	}
	signed, _, err := lp(signer)
	if err != nil {
		return nil, bad
	}
	_, rest, err := lp(signed) // digests
	if err != nil {
		return nil, bad
	}
	certs, _, err := lp(rest)
	if err != nil {
		return nil, bad
	}
	cert, _, err := lp(certs)
	if err != nil || len(cert) == 0 {
		return nil, bad
	}
	return cert, nil
}

// v1Cert reads the certificate from META-INF/*.RSA, *.DSA or *.EC, a PKCS#7
// SignedData structure.
func v1Cert(path string) ([]byte, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, errors.New("that file isn't an APK")
	}
	defer zr.Close()
	for _, f := range zr.File {
		name := strings.ToUpper(f.Name)
		if !strings.HasPrefix(name, "META-INF/") || strings.Count(name, "/") != 1 {
			continue
		}
		if !strings.HasSuffix(name, ".RSA") && !strings.HasSuffix(name, ".DSA") && !strings.HasSuffix(name, ".EC") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(rc, 1<<20))
		rc.Close()
		if err != nil {
			return nil, err
		}
		return pkcs7FirstCert(data)
	}
	return nil, errNoSignature
}

func pkcs7FirstCert(data []byte) ([]byte, error) {
	bad := errors.New("the APK's signature is damaged")
	var ci struct {
		Type    asn1.ObjectIdentifier
		Content asn1.RawValue `asn1:"explicit,tag:0"`
	}
	if _, err := asn1.Unmarshal(data, &ci); err != nil {
		return nil, bad
	}
	var sd asn1.RawValue // SignedData SEQUENCE
	if _, err := asn1.Unmarshal(ci.Content.Bytes, &sd); err != nil {
		return nil, bad
	}
	rest := sd.Bytes
	for len(rest) > 0 {
		var v asn1.RawValue
		var err error
		if rest, err = asn1.Unmarshal(rest, &v); err != nil {
			return nil, bad
		}
		if v.Class == asn1.ClassContextSpecific && v.Tag == 0 { // certificates
			var cert asn1.RawValue
			if _, err := asn1.Unmarshal(v.Bytes, &cert); err != nil {
				return nil, bad
			}
			return cert.FullBytes, nil
		}
	}
	return nil, errNoSignature
}

// apkPackageName reads the package attribute of <manifest> from the APK's
// binary AndroidManifest.xml.
func apkPackageName(path string) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", errors.New("that file isn't an APK")
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != "AndroidManifest.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		data, err := io.ReadAll(io.LimitReader(rc, 16<<20))
		rc.Close()
		if err != nil {
			return "", err
		}
		return manifestPackage(data)
	}
	return "", errors.New("the APK has no manifest")
}

func manifestPackage(b []byte) (string, error) {
	bad := errors.New("the APK's manifest couldn't be read")
	u16 := func(o int) int { return int(binary.LittleEndian.Uint16(b[o:])) }
	u32 := func(o int) int { return int(binary.LittleEndian.Uint32(b[o:])) }
	if len(b) < 8 || u16(0) != 0x0003 {
		return "", bad
	}
	var pool []string
	for off := u16(2); off+8 <= len(b); {
		typ, hsize, size := u16(off), u16(off+2), u32(off+4)
		if size < 8 || off+size > len(b) {
			return "", bad
		}
		chunk := b[off : off+size]
		switch typ {
		case 0x0001: // string pool
			p, err := readStringPool(chunk, hsize)
			if err != nil {
				return "", bad
			}
			pool = p
		case 0x0102: // start element
			if hsize+20 > len(chunk) || pool == nil {
				return "", bad
			}
			str := func(i int) string {
				if i >= 0 && i < len(pool) {
					return pool[i]
				}
				return ""
			}
			ext := hsize
			if str(int(int32(binary.LittleEndian.Uint32(chunk[ext+4:])))) != "manifest" {
				return "", bad
			}
			attrStart := int(binary.LittleEndian.Uint16(chunk[ext+8:]))
			attrSize := int(binary.LittleEndian.Uint16(chunk[ext+10:]))
			count := int(binary.LittleEndian.Uint16(chunk[ext+12:]))
			for i := 0; i < count; i++ {
				a := ext + attrStart + i*attrSize
				if attrSize < 20 || a+20 > len(chunk) {
					return "", bad
				}
				if str(int(binary.LittleEndian.Uint32(chunk[a+4:]))) != "package" {
					continue
				}
				if v := str(int(int32(binary.LittleEndian.Uint32(chunk[a+8:])))); v != "" {
					return v, nil
				}
				if chunk[a+15] == 0x03 { // typed string value
					return str(int(binary.LittleEndian.Uint32(chunk[a+16:]))), nil
				}
			}
			return "", bad
		}
		off += size
	}
	return "", bad
}

func readStringPool(c []byte, hsize int) ([]string, error) {
	if hsize < 28 || len(c) < hsize {
		return nil, errors.New("short pool")
	}
	count := int(binary.LittleEndian.Uint32(c[8:]))
	utf8 := binary.LittleEndian.Uint32(c[16:])&0x100 != 0
	start := int(binary.LittleEndian.Uint32(c[20:]))
	if count > 1<<20 || hsize+count*4 > len(c) || start > len(c) {
		return nil, errors.New("bad pool")
	}
	out := make([]string, count)
	for i := range out {
		o := start + int(binary.LittleEndian.Uint32(c[hsize+i*4:]))
		if o >= len(c) {
			return nil, errors.New("bad string")
		}
		s, err := poolString(c[o:], utf8)
		if err != nil {
			return nil, err
		}
		out[i] = s
	}
	return out, nil
}

func poolString(b []byte, utf8 bool) (string, error) {
	if utf8 {
		skip := func(b []byte) (int, []byte, error) {
			if len(b) < 1 {
				return 0, nil, errors.New("short")
			}
			if b[0]&0x80 != 0 {
				if len(b) < 2 {
					return 0, nil, errors.New("short")
				}
				return int(b[0]&0x7f)<<8 | int(b[1]), b[2:], nil
			}
			return int(b[0]), b[1:], nil
		}
		_, b, err := skip(b) // length in characters
		if err != nil {
			return "", err
		}
		n, b, err := skip(b) // length in bytes
		if err != nil || n > len(b) {
			return "", errors.New("short")
		}
		return string(b[:n]), nil
	}
	if len(b) < 2 {
		return "", errors.New("short")
	}
	n := int(binary.LittleEndian.Uint16(b))
	b = b[2:]
	if n&0x8000 != 0 {
		if len(b) < 2 {
			return "", errors.New("short")
		}
		n = (n&0x7fff)<<16 | int(binary.LittleEndian.Uint16(b))
		b = b[2:]
	}
	if n*2 > len(b) {
		return "", errors.New("short")
	}
	u := make([]uint16, n)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(b[i*2:])
	}
	return string(utf16.Decode(u)), nil
}

// sameCert reports whether two DER certificates are identical.
func sameCert(a, b []byte) bool { return len(a) > 0 && bytes.Equal(a, b) }

// certFingerprint is a short SHA-256 fingerprint for messages and logs.
func certFingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	return fmt.Sprintf("%X", sum[:8])
}
