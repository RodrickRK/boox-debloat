package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestNeverTouchIsNotRemovable(t *testing.T) {
	c := testCatalog(t)
	for _, list := range [][]App{c.Remove, c.Disable, c.Google} {
		for _, a := range list {
			if neverTouch[a.ID] {
				t.Errorf("%s is on the never-touch list but can be changed", a.ID)
			}
		}
	}
	for _, id := range []string{"com.google.android.webview", "com.android.permissioncontroller", "com.android.packageinstaller"} {
		if !c.kept(id) {
			t.Errorf("%s must be kept", id)
		}
	}
}

func TestGoogleCatalog(t *testing.T) {
	c := testCatalog(t)
	core := 0
	for _, a := range c.Google {
		if a.Core {
			core++
		}
		if c.group(a.ID) != groupGoogle {
			t.Errorf("%s should be in the Google group", a.ID)
		}
	}
	if core != len(playPackages) {
		t.Fatalf("want the %d Play packages as core, got %d", len(playPackages), core)
	}
	for _, id := range playPackages {
		if c.group(id) != groupGoogle {
			t.Errorf("%s should be a Google app, not %q", id, c.group(id))
		}
	}
}

func TestDegoogleChanges(t *testing.T) {
	c := testCatalog(t)
	states := map[string]string{
		"com.android.vending":            stateEnabled,
		"com.google.android.gms":         stateEnabled,
		"com.google.android.gsf":         stateDisabled,
		"com.google.android.safetycore":  stateEnabled,
		"com.google.android.contactkeys": stateEnabled,
		"com.google.android.verifier":    stateMissing,
	}
	got, err := degoogleChanges(c, states, []string{"com.google.android.contactkeys"})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, ch := range got {
		// Only Play services and the framework keep their data. Keeping
		// Play Store's makes Android refuse an older Play Store APK.
		want := actRemove
		if ch.ID == "com.google.android.gms" || ch.ID == "com.google.android.gsf" {
			want = actRemoveKeep
		}
		if ch.Action != want {
			t.Errorf("%s: want %s, got %s", ch.ID, want, ch.Action)
		}
		ids = append(ids, ch.ID)
	}
	want := "com.google.android.safetycore com.android.vending com.google.android.gms com.google.android.gsf"
	if strings.Join(ids, " ") != want {
		t.Fatalf("got %v, want extras first, kept and missing apps skipped: %s", ids, want)
	}

	if _, err := degoogleChanges(c, states, []string{"com.google.android.gms"}); err == nil {
		t.Error("keeping Play services on its own should be refused")
	}
	if _, err := degoogleChanges(c, states, []string{"com.onyx.mail"}); err == nil {
		t.Error("keeping a non-Google app should be refused")
	}
}

func TestGoogleOnlyThroughDegoogle(t *testing.T) {
	c := testCatalog(t)
	if err := validate(c, []Change{{"com.google.android.gms", actRemove}}); err == nil {
		t.Error("removing Play services through Apply should be refused")
	}
	if err := validate(c, []Change{{"com.google.android.safetycore", actRemoveKeep}}); err == nil {
		t.Error("the page must not be able to send remove-keep-data")
	}
	if err := validate(c, []Change{{"com.google.android.gms", actRestore}}); err != nil {
		t.Errorf("restoring Google should be allowed: %v", err)
	}
}

func TestApplyChangeRefusesKeptApps(t *testing.T) {
	c := testCatalog(t)
	d := newDemoBackend(c)
	for _, ch := range []Change{
		{"com.google.android.webview", actRemoveKeep},
		{"com.onyx.android.ksync", actRemove},
		{"com.android.permissioncontroller", actDisable},
	} {
		if r := applyChange(d, c, "DEMO-C69", ch); r.OK {
			t.Errorf("%v went through", ch)
		}
	}
}

func TestRestoreOfAddedAppIsReportedAsFailed(t *testing.T) {
	c := testCatalog(t)
	d := newDemoBackend(c)
	id := "com.google.android.safetycore"
	if r := applyChange(d, c, "DEMO-C69", Change{id, actRemoveKeep}); !r.OK {
		t.Fatalf("remove: %s", r.Message)
	}
	states, err := readStates(d, "DEMO-C69", c)
	if err != nil {
		t.Fatal(err)
	}
	if states[id] != stateMissing {
		t.Fatalf("an added app removed for user 0 has no APK left; want missing, got %s", states[id])
	}
	if r := applyChange(d, c, "DEMO-C69", Change{id, actRestore}); r.OK {
		t.Fatal("install-existing claims success, but there's nothing to restore")
	}
}

func TestDegoogleEndpointAndRestoreAll(t *testing.T) {
	be := newDemoBackend(testCatalog(t))
	s := testServer(t, be)
	s.demo = true // no backup files from tests

	rec := call(s, http.MethodPost, "/api/degoogle", `{"keep":["com.google.android.verifier"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("degoogle: %d %s", rec.Code, rec.Body)
	}
	var res applyResp
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	for _, r := range res.Results {
		if !r.OK {
			t.Errorf("%s: %s", r.ID, r.Message)
		}
		if r.ID == "com.google.android.verifier" {
			t.Error("verifier was kept, but got removed")
		}
	}
	// Demo firmware: Play Store missing, Play services and the framework
	// turned off, three extras on (one kept).
	if len(res.Results) != 4 {
		t.Fatalf("want 4 removals, got %+v", res.Results)
	}
	if be.state["com.google.android.gms"] != "removed" || be.state["com.google.android.verifier"] != "enabled" {
		t.Fatalf("states after De-Google: %v %v", be.state["com.google.android.gms"], be.state["com.google.android.verifier"])
	}

	if rec := call(s, http.MethodPost, "/api/restore-all", ""); rec.Code != http.StatusOK {
		t.Fatalf("restore-all: %d %s", rec.Code, rec.Body)
	}
	if be.state["com.google.android.gms"] != "removed" {
		t.Error("Restore all must leave Google alone")
	}

	if rec := call(s, http.MethodPost, "/api/enable-play", ""); rec.Code != http.StatusOK {
		t.Fatalf("enable-play: %d %s", rec.Code, rec.Body)
	}
	if be.state["com.google.android.gms"] != "enabled" || be.state["com.google.android.gsf"] != "enabled" {
		t.Error("Bring Google back should turn Play services and the framework on again")
	}

	rec = call(s, http.MethodGet, "/api/apps", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"googleAccount":true`) || !strings.Contains(rec.Body.String(), `"google":[`) {
		t.Fatalf("apps: %d %s", rec.Code, rec.Body)
	}
}
