package main

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

type kioskView struct {
	Heading      string
	Instructions string
	InputLabel   string
	HasLogo      bool
	Message      string
	Status       string
	ThemeMode    string
	ThemeColor   string
}

func (a *app) handleKiosk(w http.ResponseWriter, r *http.Request) {
	_, hasLogo := a.logoPath()
	view := kioskView{
		Heading:      a.kioskHeading(),
		Instructions: a.kioskInstructions(),
		InputLabel:   a.kioskInputLabel(),
		HasLogo:      hasLogo,
		Message:      r.URL.Query().Get("msg"),
		Status:       r.URL.Query().Get("status"),
		ThemeMode:    themeModeAttr(a.themeMode()),
		ThemeColor:   a.themeColor(),
	}
	if err := a.tpl.ExecuteTemplate(w, "kiosk.html", view); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (a *app) kioskHeading() string      { return configOr(a.db, cfgKioskHeading, a.siteName()) }
func (a *app) kioskInstructions() string { return configOr(a.db, cfgKioskInstructions, "") }
func (a *app) kioskInputLabel() string   { return configOr(a.db, cfgKioskInputLabel, "Enter your ID") }

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
	q := url.Values{"msg": {msg}, "status": {status}}
	http.Redirect(w, r, "/?"+q.Encode(), http.StatusSeeOther)
}

func kioskMessage(res scanResult, loc *time.Location) (msg, status string) {
	switch res.Status {
	case statusIn:
		return "Welcome, " + res.Name + " — logged IN at " + res.At.In(loc).Format("3:04 PM"), "in"
	case statusOut:
		return "Goodbye, " + res.Name + " — logged OUT at " + res.At.In(loc).Format("3:04 PM"), "out"
	case statusDebounced:
		return res.Name + " — already recorded a moment ago", "info"
	case statusInactive:
		return "This ID is inactive — please see staff", "error"
	default:
		return "ID not recognized — please see staff", "error"
	}
}
