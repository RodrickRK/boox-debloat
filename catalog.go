package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed apps.json
var appsJSON []byte

// App is one Android package the tool knows about.
type App struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Why  string `json:"why"`
	Core bool   `json:"core,omitempty"` // Google: removed and restored as a set
	// KeepData: De-Google keeps this app's data (the GSF ID lives in
	// Services Framework's). Apps installed after the firmware, like Play
	// Store, mustn't keep theirs: Android then refuses an older APK.
	KeepData bool `json:"keepData,omitempty"`
}

// Catalog holds the app lists from apps.json.
type Catalog struct {
	Model     string `json:"model"`
	Remove    []App  `json:"remove"`
	Disable   []App  `json:"disable"`
	Protected []App  `json:"protected"`
	Google    []App  `json:"google"` // only removed through De-Google
}

const (
	groupRemove    = "remove"
	groupDisable   = "disable"
	groupProtected = "protected"
	groupGoogle    = "google"
)

// neverTouch lists packages the tablet needs to work properly. Nothing in
// this tool removes or turns them off, whatever a request asks for. It's
// checked right before each command, on top of the catalog's own rules.
var neverTouch = map[string]bool{
	"android":                                 true,
	"com.android.systemui":                    true,
	"com.android.settings":                    true,
	"com.android.phone":                       true,
	"com.android.shell":                       true,
	"com.google.android.webview":              true, // the only WebView on the C69
	"com.android.webview":                     true,
	"com.android.permissioncontroller":        true,
	"com.google.android.permissioncontroller": true,
	"com.android.packageinstaller":            true,
	"com.google.android.packageinstaller":     true,
	"com.android.ext.services":                true,
	"com.google.android.ext.services":         true,
	"com.android.networkstack":                true,
	"com.google.android.networkstack":         true,
	"com.android.modulemetadata":              true,
	"com.google.android.modulemetadata":       true,
	"com.android.inputmethod.latin":           true,
	"com.google.android.inputmethod.latin":    true,
	"com.android.providers.settings":          true,
	"com.android.providers.downloads":         true,
	"com.android.externalstorage":             true,
	"com.android.documentsui":                 true,
}

func loadCatalog() (*Catalog, error) {
	var c Catalog
	if err := json.Unmarshal(appsJSON, &c); err != nil {
		return nil, fmt.Errorf("reading apps.json: %w", err)
	}
	return &c, nil
}

// group returns which list an app is in, or "" if the tool doesn't know it.
func (c *Catalog) group(id string) string {
	for _, a := range c.Remove {
		if a.ID == id {
			return groupRemove
		}
	}
	for _, a := range c.Disable {
		if a.ID == id {
			return groupDisable
		}
	}
	for _, a := range c.Protected {
		if a.ID == id {
			return groupProtected
		}
	}
	for _, a := range c.Google {
		if a.ID == id {
			return groupGoogle
		}
	}
	return ""
}

// kept reports whether a package must never be removed or turned off.
func (c *Catalog) kept(id string) bool {
	return neverTouch[id] || c.group(id) == groupProtected
}

// all returns every app in the catalog.
func (c *Catalog) all() []App {
	out := make([]App, 0, len(c.Remove)+len(c.Disable)+len(c.Protected)+len(c.Google))
	out = append(out, c.Remove...)
	out = append(out, c.Disable...)
	out = append(out, c.Protected...)
	out = append(out, c.Google...)
	return out
}
