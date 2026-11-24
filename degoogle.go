package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// De-Google removes Google from the tablet for user 0. Play services and
// Services Framework keep their data, so the GSF ID and Play Protect
// registration survive a restore. The Google account doesn't: Android
// deletes accounts once Play services, their sign-in handler, is gone. The
// core apps (Play Store, Play services, Services Framework) always go
// together; the extras installed by Play go too unless the user keeps them.
// Turn on Google Play (handleEnablePlay) is the way back.

// degoogleChanges works out what to remove. keep holds extras the user
// wants to keep; core apps can't be kept. Extras go first and Services
// Framework last, so nothing is left running without what it needs.
func degoogleChanges(c *Catalog, states map[string]string, keep []string) ([]Change, error) {
	kept := map[string]bool{}
	for _, id := range keep {
		if c.group(id) != groupGoogle {
			return nil, errBadKeep(id)
		}
		for _, a := range c.Google {
			if a.ID == id && a.Core {
				return nil, errCoreKeep(a.Name)
			}
		}
		kept[id] = true
	}
	var extras, core []Change
	for _, a := range c.Google {
		if kept[a.ID] || !installed(states[a.ID]) || c.kept(a.ID) {
			continue
		}
		ch := Change{ID: a.ID, Action: actRemove}
		if a.KeepData {
			ch.Action = actRemoveKeep
		}
		if a.Core {
			core = append(core, ch)
		} else {
			extras = append(extras, ch)
		}
	}
	return append(extras, core...), nil
}

func installed(state string) bool { return state == stateEnabled || state == stateDisabled }

type keepError struct{ msg string }

func (e keepError) Error() string { return e.msg }

func errBadKeep(id string) error {
	return keepError{id + " isn't one of the Google apps."}
}

func errCoreKeep(name string) error {
	return keepError{name + " is part of Google itself, so it can't be kept on its own."}
}

func (s *Server) handleDegoogle(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Keep []string `json:"keep"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dev, ok := s.readyDevice(w, true)
	if !ok {
		return
	}
	states, err := readStates(s.be, dev.Serial, s.cat)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	changes, err := degoogleChanges(s.cat, states, body.Keep)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(changes) == 0 {
		writeJSON(w, http.StatusOK, applyResp{Results: []Result{}})
		return
	}
	s.log.add("De-Googling: removing %d apps, keeping their data", len(changes))
	s.execute(w, dev, changes)
}

// googleAccount reports whether a Google account is signed in. The tablet
// only answers with a count, so no account name reaches the activity log.
func (s *Server) googleAccount(serial string) bool {
	out, _ := s.be.Shell(serial, `dumpsys account | grep -c "Account {name=.*type=com.google}" || true`)
	n, _ := strconv.Atoi(strings.TrimSpace(out))
	return n > 0
}
