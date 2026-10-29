package main

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const maxLogoBytes = 2 << 20

var allowedLogoExt = map[string]bool{
	".png": true, ".svg": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
}

type settingsData struct {
	SiteName          string
	Timezone          string
	Debounce          string
	AutoCheckout      bool
	AutoCheckoutAt    string
	CommonTimezones   []string
	KioskHeading      string
	KioskInstructions string
	DataDir           string
	LogoFile          string
}

func (a *app) handleSettings(w http.ResponseWriter, r *http.Request) {
	logoFile := ""
	if p, ok := a.logoPath(); ok {
		logoFile = filepath.Base(p)
	}
	v := a.adminBase(r, "settings")
	v.Data = settingsData{
		SiteName:          a.siteName(),
		Timezone:          configOr(a.db, cfgTimezone, "UTC"),
		Debounce:          configOr(a.db, cfgDebounce, "5"),
		AutoCheckout:      configOr(a.db, cfgAutoCheckoutEnabled, "0") == "1",
		AutoCheckoutAt:    configOr(a.db, cfgAutoCheckoutTime, ""),
		CommonTimezones:   commonTimezones,
		KioskHeading:      configOr(a.db, cfgKioskHeading, ""),
		KioskInstructions: configOr(a.db, cfgKioskInstructions, ""),
		DataDir:           a.dataDir,
		LogoFile:          logoFile,
	}
	a.render(w, "settings.html", v)
}

func (a *app) handleSettingsSave(w http.ResponseWriter, r *http.Request) {
	if !a.checkCSRF(r) {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if name := strings.TrimSpace(r.FormValue("site_name")); name != "" {
		_ = setConfig(a.db, cfgSiteName, name)
	}
	if tz := strings.TrimSpace(r.FormValue("timezone")); tz != "" {
		if _, err := time.LoadLocation(tz); err != nil {
			http.Redirect(w, r, "/admin/settings?err="+url.QueryEscape("Unknown timezone: "+tz), http.StatusSeeOther)
			return
		}
		_ = setConfig(a.db, cfgTimezone, tz)
	}
	if n, err := strconv.Atoi(strings.TrimSpace(r.FormValue("debounce"))); err == nil && n >= 0 {
		_ = setConfig(a.db, cfgDebounce, strconv.Itoa(n))
	}
	enabled := r.FormValue("auto_checkout") == "on"
	_ = setConfig(a.db, cfgAutoCheckoutEnabled, boolConfig(enabled))
	if at := strings.TrimSpace(r.FormValue("auto_checkout_at")); validClock(at) {
		_ = setConfig(a.db, cfgAutoCheckoutTime, at)
	}
	_ = setConfig(a.db, cfgKioskHeading, strings.TrimSpace(r.FormValue("kiosk_heading")))
	instructions := strings.ReplaceAll(r.FormValue("kiosk_instructions"), "\r\n", "\n")
	_ = setConfig(a.db, cfgKioskInstructions, strings.TrimSpace(instructions))
	http.Redirect(w, r, "/admin/settings?flash=Settings+saved", http.StatusSeeOther)
}

func (a *app) handlePasswordChange(w http.ResponseWriter, r *http.Request) {
	if !a.checkCSRF(r) {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if !a.checkPassword(r.FormValue("current")) {
		http.Redirect(w, r, "/admin/settings?err=Current+password+is+incorrect", http.StatusSeeOther)
		return
	}
	if err := validatePassword(r.FormValue("new"), r.FormValue("confirm")); err != nil {
		http.Redirect(w, r, "/admin/settings?err="+err.Error(), http.StatusSeeOther)
		return
	}
	if err := a.setPassword(r.FormValue("new")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/settings?flash=Password+changed", http.StatusSeeOther)
}

// handleLogoUpload saves an uploaded image into the data dir as logo.<ext>,
// replacing any existing logo so there is never more than one.
func (a *app) handleLogoUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxLogoBytes+4096)
	if err := r.ParseMultipartForm(maxLogoBytes); err != nil {
		http.Redirect(w, r, "/admin/settings?err="+url.QueryEscape("File too large (max 2 MB)"), http.StatusSeeOther)
		return
	}
	if !a.checkCSRF(r) {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("logo")
	if err != nil {
		http.Redirect(w, r, "/admin/settings?err=No+file+selected", http.StatusSeeOther)
		return
	}
	defer file.Close()
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if !allowedLogoExt[ext] {
		http.Redirect(w, r, "/admin/settings?err="+url.QueryEscape("Unsupported file type"), http.StatusSeeOther)
		return
	}
	data, err := io.ReadAll(file)
	if err != nil {
		http.Redirect(w, r, "/admin/settings?err="+url.QueryEscape("Could not read file"), http.StatusSeeOther)
		return
	}
	// SVG is text and never sniffs as image/*; raster formats must.
	if ext != ".svg" && !strings.HasPrefix(http.DetectContentType(data), "image/") {
		http.Redirect(w, r, "/admin/settings?err="+url.QueryEscape("That file is not an image"), http.StatusSeeOther)
		return
	}
	a.removeLogo()
	if err := os.WriteFile(filepath.Join(a.dataDir, "logo"+ext), data, 0o644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/settings?flash=Logo+uploaded", http.StatusSeeOther)
}

func (a *app) handleLogoDelete(w http.ResponseWriter, r *http.Request) {
	if !a.checkCSRF(r) {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	a.removeLogo()
	http.Redirect(w, r, "/admin/settings?flash=Logo+removed", http.StatusSeeOther)
}

func (a *app) removeLogo() {
	for _, n := range logoNames {
		_ = os.Remove(filepath.Join(a.dataDir, n))
	}
}

// handleBackup streams a consistent snapshot via VACUUM INTO so a non-technical
// user can save their data without touching the filesystem or WAL files.
func (a *app) handleBackup(w http.ResponseWriter, r *http.Request) {
	tmp := filepath.Join(os.TempDir(), "ll-backup-"+randomHex(8)+".db")
	if _, err := a.db.Exec(`VACUUM INTO ?`, tmp); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.Remove(tmp)
	f, err := os.Open(tmp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="library-login-backup.db"`)
	http.ServeContent(w, r, "library-login-backup.db", time.Now(), f)
}

func boolConfig(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

var commonTimezones = []string{
	"UTC",
	"America/New_York",
	"America/Chicago",
	"America/Denver",
	"America/Los_Angeles",
	"America/Anchorage",
	"Pacific/Honolulu",
	"Europe/London",
}
