package main

import (
	"image"
	"image/color"
	"testing"
)

func testCatalog(t *testing.T) *Catalog {
	t.Helper()
	c, err := loadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCatalogHasNoOverlaps(t *testing.T) {
	c := testCatalog(t)
	seen := map[string]string{}
	for _, list := range []struct {
		name string
		apps []App
	}{{"remove", c.Remove}, {"disable", c.Disable}, {"protected", c.Protected}} {
		for _, a := range list.apps {
			if prev, ok := seen[a.ID]; ok {
				t.Errorf("%s is in both %s and %s", a.ID, prev, list.name)
			}
			seen[a.ID] = list.name
			if a.Name == "" || a.Why == "" {
				t.Errorf("%s needs a name and a reason", a.ID)
			}
		}
	}
}

func TestKsyncIsProtected(t *testing.T) {
	if g := testCatalog(t).group("com.onyx.android.ksync"); g != groupProtected {
		t.Fatalf("ksync must be protected, got %q", g)
	}
}

func TestValidate(t *testing.T) {
	c := testCatalog(t)
	cases := []struct {
		change Change
		ok     bool
	}{
		{Change{"com.onyx.mail", actRemove}, true},
		{Change{"com.onyx.mail", actRestore}, true},
		{Change{"com.android.nfc", actDisable}, true},
		{Change{"com.android.nfc", actRemove}, false},
		{Change{"com.onyx.android.ksync", actRemove}, false},
		{Change{"com.onyx.android.ksync", actDisable}, false},
		{Change{"com.onyx.android.ksync", actRestore}, true},
		{Change{"com.onyx", actRemove}, false},
		{Change{"com.example.unknown", actRestore}, false},
		{Change{"com.onyx.mail", "wipe"}, false},
	}
	for _, tc := range cases {
		err := validate(c, []Change{tc.change})
		if (err == nil) != tc.ok {
			t.Errorf("%v: got err=%v, want ok=%v", tc.change, err, tc.ok)
		}
	}
	if validate(c, nil) == nil {
		t.Error("an empty change list should be rejected")
	}
}

func TestDemoApplyAndRestore(t *testing.T) {
	c := testCatalog(t)
	d := newDemoBackend(c)
	serial := "DEMO-C69"

	states, err := readStates(d, serial, c)
	if err != nil {
		t.Fatal(err)
	}
	if states["com.onyx.android.ksync"] != stateRemoved {
		t.Fatalf("demo should start with ksync removed, got %s", states["com.onyx.android.ksync"])
	}

	for _, ch := range []Change{
		{"com.onyx.mail", actRemove},
		{"com.android.nfc", actDisable},
		{"com.onyx.android.ksync", actRestore},
	} {
		if r := applyChange(d, serial, ch); !r.OK {
			t.Fatalf("%v failed: %s", ch, r.Message)
		}
	}
	states, _ = readStates(d, serial, c)
	want := map[string]string{
		"com.onyx.mail":          stateRemoved,
		"com.android.nfc":        stateDisabled,
		"com.onyx.android.ksync": stateEnabled,
	}
	for id, s := range want {
		if states[id] != s {
			t.Errorf("%s: got %s, want %s", id, states[id], s)
		}
	}
}

func TestParseDevices(t *testing.T) {
	out := "* daemon started successfully\nList of devices attached\n2308C69HDM09563\tdevice usb:1-1 product:C69_M_STAR\nABC\tunauthorized\n\n"
	got := parseDevices(out)
	if len(got) != 2 || got[0].Serial != "2308C69HDM09563" || got[0].State != "device" || got[1].State != "unauthorized" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestFitToScreen(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 3000, 2000)) // wide and black
	for i := 3; i < len(src.Pix); i += 4 {
		src.Pix[i] = 255
	}
	out := fitToScreen(src, screenW, screenH)
	if b := out.Bounds(); b.Dx() != screenW || b.Dy() != screenH {
		t.Fatalf("got %v", b)
	}
	if out.GrayAt(5, 5).Y != 255 {
		t.Error("letterbox area should be white")
	}
	if out.GrayAt(screenW/2, screenH/2).Y != 0 {
		t.Error("picture area should be black")
	}
}

func TestTransparentIsWhite(t *testing.T) {
	if lumaOnWhite(color.RGBA{}) != 255 {
		t.Error("fully transparent pixels should become white")
	}
}

func TestSafeName(t *testing.T) {
	for in, want := range map[string]string{"My Photo.v2": "my-photo-v2", "../../etc": "etc", "": "device"} {
		if got := safeName(in); got != want {
			t.Errorf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
}
