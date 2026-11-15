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
const maxSoundBytes = 1 << 20

var allowedLogoExt = map[string]bool{
	".png": true, ".svg": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
}

var allowedSoundExt = map[string]bool{".wav": true, ".mp3": true, ".ogg": true}

type settingsData struct {
	SiteName          string
	Timezone          string
	Debounce          string
	AutoCheckout      bool
	AutoCheckoutAt    string
	CommonTimezones   []string
	KioskHeading      string
	KioskInstructions string
	KioskInputLabel   string
	KioskButtonLabel  string
	DataDir           string
	LogoFile          string
	ThemeMode         string
	ThemeColor        string
	Accents           []accent
	SoundEnabled      bool
	Sounds            []soundSetting
}

// soundSetting describes one kiosk sound in the admin Audio panel: whether a
// custom file has replaced the packaged default.
type soundSetting struct {
	Event  string
	Label  string
	Custom bool
}

var kioskSoundLabels = []struct{ Event, Label string }{
	{"login", "Login"},
	{"logout", "Logout"},
	{"error", "Failure"},
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
		KioskInputLabel:   configOr(a.db, cfgKioskInputLabel, "Enter your ID"),
		KioskButtonLabel:  configOr(a.db, cfgKioskButtonLabel, "Log in / out"),
		DataDir:           a.dataDir,
		LogoFile:          logoFile,
		ThemeMode:         a.themeMode(),
		ThemeColor:        a.themeColor(),
		Accents:           accents,
		SoundEnabled:      a.kioskSoundEnabled(),
		Sounds:            a.soundSettings(),
	}
	a.render(w, "settings.html", v)
}

func (a *app) handleGeneralSettingsSave(w http.ResponseWriter, r *http.Request) {
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

	http.Redirect(w, r, "/admin/settings?flash=Settings+saved", http.StatusSeeOther)
}

func (a *app) handleAppearanceSave(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxLogoBytes+4096)
	if err := r.ParseMultipartForm(maxLogoBytes); err != nil {
		http.Redirect(w, r, "/admin/settings?err="+url.QueryEscape("File too large (max 2 MB)"), http.StatusSeeOther)
		return
	}
	if !a.checkCSRF(r) {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if m := r.FormValue("theme_mode"); themeModes[m] {
		_ = setConfig(a.db, cfgThemeMode, m)
	}
	if c := r.FormValue("theme_color"); validAccent(c) {
		_ = setConfig(a.db, cfgThemeColor, c)
	}

	_ = setConfig(a.db, cfgKioskHeading, strings.TrimSpace(r.FormValue("kiosk_heading")))

	instructions := strings.ReplaceAll(r.FormValue("kiosk_instructions"), "\r\n", "\n")
	_ = setConfig(a.db, cfgKioskInstructions, strings.TrimSpace(instructions))

	_ = setConfig(a.db, cfgKioskInputLabel, strings.TrimSpace(r.FormValue("kiosk_input_label")))

	_ = setConfig(a.db, cfgKioskButtonLabel, strings.TrimSpace(r.FormValue("kiosk_button_label")))

	if r.FormValue("delete_logo") == "on" {
		a.deleteLogo()
	} else if logoErr := a.saveLogo(r); logoErr != "" {
		http.Redirect(w, r, "/admin/settings?err="+logoErr, http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/admin/settings?flash=Appearance+saved", http.StatusSeeOther)
}

func (a *app) soundSettings() []soundSetting {
	out := make([]soundSetting, len(kioskSoundLabels))
	for i, s := range kioskSoundLabels {
		_, custom := a.soundPath(s.Event)
		out[i] = soundSetting{Event: s.Event, Label: s.Label, Custom: custom}
	}
	return out
}

func (a *app) handleAudioSave(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, int64(len(soundEvents))*maxSoundBytes+4096)
	if err := r.ParseMultipartForm(maxSoundBytes); err != nil {
		http.Redirect(w, r, "/admin/settings?err="+url.QueryEscape("File too large (max 1 MB each)"), http.StatusSeeOther)
		return
	}
	if !a.checkCSRF(r) {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	_ = setConfig(a.db, cfgKioskSoundEnabled, boolConfig(r.FormValue("kiosk_sound") == "on"))

	for event := range soundEvents {
		if r.FormValue("delete_sound_"+event) == "on" {
			a.deleteSound(event)
			continue
		}
		if soundErr := a.saveSound(r, event); soundErr != "" {
			http.Redirect(w, r, "/admin/settings?err="+soundErr, http.StatusSeeOther)
			return
		}
	}

	http.Redirect(w, r, "/admin/settings?flash=Audio+saved+-+Reload+the+kiosk+page+to+hear+changes", http.StatusSeeOther)
}

// saveSound stores an uploaded sound as <event>.<ext> in the data dir, replacing
// any existing custom sound for that event. It assumes the multipart form is
// already parsed; a missing file is not an error. It returns a URL-escaped
// message on a real failure, or "" on success or no upload.
func (a *app) saveSound(r *http.Request, event string) string {
	file, header, err := r.FormFile("sound_" + event)
	if err != nil {
		return ""
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if !allowedSoundExt[ext] {
		return url.QueryEscape("Unsupported audio type (use .wav, .mp3, or .ogg)")
	}
	if header.Size > maxSoundBytes {
		return url.QueryEscape("File too large (max 1 MB each)")
	}

	data, err := io.ReadAll(file)
	if err != nil {
		return url.QueryEscape("Could not read file")
	}
	if !validAudio(ext, data) {
		return url.QueryEscape("That file is not a valid " + ext[1:] + " audio file")
	}

	a.deleteSound(event)
	if err := os.WriteFile(filepath.Join(a.dataDir, event+ext), data, 0o644); err != nil {
		return url.QueryEscape("Internal server error")
	}
	return ""
}

func (a *app) deleteSound(event string) {
	for _, ext := range soundExts {
		_ = os.Remove(filepath.Join(a.dataDir, event+ext))
	}
}

// validAudio does a light magic-byte check so an uploaded file's contents match
// its extension; it is a sanity gate, not a full decoder.
func validAudio(ext string, data []byte) bool {
	switch ext {
	case ".wav":
		return len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WAVE"
	case ".ogg":
		return len(data) >= 4 && string(data[:4]) == "OggS"
	case ".mp3":
		return len(data) >= 3 && (string(data[:3]) == "ID3" || (data[0] == 0xFF && data[1]&0xE0 == 0xE0))
	}
	return false
}

func (a *app) handleBehaviorSave(w http.ResponseWriter, r *http.Request) {
	if !a.checkCSRF(r) {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if n, err := strconv.Atoi(strings.TrimSpace(r.FormValue("debounce"))); err == nil && n >= 0 {
		_ = setConfig(a.db, cfgDebounce, strconv.Itoa(n))
	}

	enabled := r.FormValue("auto_checkout") == "on"
	_ = setConfig(a.db, cfgAutoCheckoutEnabled, boolConfig(enabled))

	if at := strings.TrimSpace(r.FormValue("auto_checkout_at")); validClock(at) {
		_ = setConfig(a.db, cfgAutoCheckoutTime, at)
	}

	http.Redirect(w, r, "/admin/settings?flash=Behavior+saved", http.StatusSeeOther)
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

// saveLogo stores an uploaded image as logo.<ext> in the data dir, replacing any
// existing logo. It assumes the multipart form is already parsed; a missing file
// is not an error, so the caller can save other fields without an upload. It
// returns a URL-escaped message on a real failure, or "" on success or no upload.
func (a *app) saveLogo(r *http.Request) string {
	file, header, err := r.FormFile("logo")
	if err != nil {
		return ""
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if !allowedLogoExt[ext] {
		return url.QueryEscape("Unsupported file type")
	}

	data, err := io.ReadAll(file)
	if err != nil {
		return url.QueryEscape("Could not read file")
	}

	// SVG is text and never sniffs as image/*; raster formats must.
	if ext != ".svg" && !strings.HasPrefix(http.DetectContentType(data), "image/") {
		return url.QueryEscape("That file is not an image")
	}

	a.deleteLogo()
	if err := os.WriteFile(filepath.Join(a.dataDir, "logo"+ext), data, 0o644); err != nil {
		return url.QueryEscape("Internal server error")
	}
	return ""
}

func (a *app) deleteLogo() {
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
