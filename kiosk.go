package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type kioskView struct {
	Heading      string
	Instructions string
	InputLabel   string
	ButtonLabel  string
	HasLogo      bool
	Message      string
	Status       string
	ThemeMode    string
	ThemeColor   string
	SoundEnabled bool
}

func (a *app) handleKiosk(w http.ResponseWriter, r *http.Request) {
	_, hasLogo := a.logoPath()
	view := kioskView{
		Heading:      a.kioskHeading(),
		Instructions: a.kioskInstructions(),
		InputLabel:   a.kioskInputLabel(),
		ButtonLabel:  a.kioskButtonLabel(),
		HasLogo:      hasLogo,
		Message:      r.URL.Query().Get("msg"),
		Status:       r.URL.Query().Get("status"),
		ThemeMode:    themeModeAttr(a.themeMode()),
		ThemeColor:   a.themeColor(),
		SoundEnabled: a.kioskSoundEnabled(),
	}
	if err := a.tpl.ExecuteTemplate(w, "kiosk.html", view); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (a *app) kioskHeading() string      { return configOr(a.db, cfgKioskHeading, a.siteName()) }
func (a *app) kioskInstructions() string { return configOr(a.db, cfgKioskInstructions, "") }
func (a *app) kioskInputLabel() string   { return configOr(a.db, cfgKioskInputLabel, "Enter your ID") }
func (a *app) kioskButtonLabel() string  { return configOr(a.db, cfgKioskButtonLabel, "Log in / out") }
func (a *app) kioskSoundEnabled() bool   { return configOr(a.db, cfgKioskSoundEnabled, "1") == "1" }

// soundEvents is the set of kiosk sound events. Each doubles as the route key and
// the data-dir basename for a custom upload (stored as <event>.<ext>).
var soundEvents = map[string]bool{"login": true, "logout": true, "error": true}

var soundExts = []string{".wav", ".mp3", ".ogg"}

// soundPath returns the custom sound file a user has supplied for event, if one
// exists in the data dir. The bool is false when only the embedded default applies.
func (a *app) soundPath(event string) (string, bool) {
	if !soundEvents[event] {
		return "", false
	}
	for _, ext := range soundExts {
		p := filepath.Join(a.dataDir, event+ext)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, true
		}
	}
	return "", false
}

// handleSound serves the sound for a kiosk event, preferring a user-supplied file
// in the data dir and falling back to the embedded default.
func (a *app) handleSound(w http.ResponseWriter, r *http.Request) {
	event := r.PathValue("event")
	if !soundEvents[event] {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if p, ok := a.soundPath(event); ok {
		http.ServeFile(w, r, p)
		return
	}
	data, err := assetsFS.ReadFile("assets/sounds/" + event + ".wav")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, event+".wav", time.Time{}, bytes.NewReader(data))
}

var logoNames = []string{"logo.png", "logo.svg", "logo.jpg", "logo.jpeg", "logo.gif", "logo.webp"}

// logoPath returns the first recognized logo file dropped into the data dir.
func (a *app) logoPath() (string, bool) {
	for _, n := range logoNames {
		p := filepath.Join(a.dataDir, n)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, true
		}
	}
	return "", false
}

func (a *app) handleLogo(w http.ResponseWriter, r *http.Request) {
	p, ok := a.logoPath()
	if !ok {
		http.NotFound(w, r)
		return
	}
	// Neutralize scripts in a directly-opened SVG; harmless for raster images.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	http.ServeFile(w, r, p)
}

func (a *app) handleSubmit(w http.ResponseWriter, r *http.Request) {
	res, err := a.recordScan(r.FormValue("id"), time.Now())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	msg, status := kioskMessage(res, a.location())
	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"msg": msg, "status": status})
		return
	}
	q := url.Values{"msg": {msg}, "status": {status}}
	http.Redirect(w, r, "/?"+q.Encode(), http.StatusSeeOther)
}

func wantsJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/json")
}

func kioskMessage(res scanResult, loc *time.Location) (msg, status string) {
	switch res.Status {
	case statusIn:
		return "Welcome, " + res.Name, "in"
	case statusOut:
		return "Goodbye, " + res.Name, "out"
	case statusDebounced:
		return res.Name + " — already recorded a moment ago", "info"
	case statusInactive:
		return "This ID is inactive — please see staff", "error"
	default:
		return "ID not recognized — please see staff", "error"
	}
}
