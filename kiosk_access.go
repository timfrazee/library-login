package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	kioskCookie = "ll_kiosk"
	kioskTTL    = 365 * 24 * time.Hour
)

type kioskGateView struct {
	Heading    string
	HasLogo    bool
	FlashErr   string
	ThemeMode  string
	ThemeColor string
}

func (a *app) kioskCode() string { return configOr(a.db, cfgKioskAccessCode, "") }

// kioskGateEnabled reports whether a pairing code gates the kiosk. An empty code
// leaves the kiosk open, preserving the default behavior.
func (a *app) kioskGateEnabled() bool { return a.kioskCode() != "" }

// signKiosk binds a pairing token to the current access code, so changing the
// code in admin revokes every device paired under the old one.
func (a *app) signKiosk(payload string) string {
	mac := hmac.New(sha256.New, a.secret())
	mac.Write([]byte("kiosk." + a.kioskCode() + "." + payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func (a *app) kioskToken() string {
	exp := strconv.FormatInt(time.Now().Add(kioskTTL).Unix(), 10)
	return exp + "." + a.signKiosk(exp)
}

func (a *app) validKioskToken(token string) bool {
	exp, sig, ok := strings.Cut(token, ".")
	if !ok {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(sig), []byte(a.signKiosk(exp))) != 1 {
		return false
	}
	ts, err := strconv.ParseInt(exp, 10, 64)
	return err == nil && time.Now().Unix() < ts
}

// isLoopback reports whether the request comes from the host itself, which is
// exempt from the gate so Local mode and the serving machine never need the code.
func isLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// requireKiosk gates the kiosk form and submission behind the pairing code. With
// no code set, or for a loopback client, it passes straight through.
func (a *app) requireKiosk(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.kioskGateEnabled() || isLoopback(r) {
			h(w, r)
			return
		}
		if c, err := r.Cookie(kioskCookie); err == nil && a.validKioskToken(c.Value) {
			h(w, r)
			return
		}
		if r.Method == http.MethodGet {
			a.renderKioskGate(w, "")
			return
		}
		http.Error(w, "this device is not paired with the kiosk", http.StatusForbidden)
	}
}

func (a *app) renderKioskGate(w http.ResponseWriter, flashErr string) {
	_, hasLogo := a.logoPath()
	view := kioskGateView{
		Heading:    a.kioskHeading(),
		HasLogo:    hasLogo,
		FlashErr:   flashErr,
		ThemeMode:  themeModeAttr(a.themeMode()),
		ThemeColor: a.themeColor(),
	}
	if err := a.tpl.ExecuteTemplate(w, "kiosk_gate.html", view); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (a *app) handleKioskUnlock(w http.ResponseWriter, r *http.Request) {
	if !a.kioskGateEnabled() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if a.kioskLocked() {
		a.renderKioskGate(w, "Too many attempts. Try again shortly.")
		return
	}
	code := strings.TrimSpace(r.FormValue("code"))
	if subtle.ConstantTimeCompare([]byte(code), []byte(a.kioskCode())) != 1 {
		a.recordKioskAttempt(false)
		a.renderKioskGate(w, "Incorrect code")
		return
	}
	a.recordKioskAttempt(true)
	http.SetCookie(w, &http.Cookie{
		Name: kioskCookie, Value: a.kioskToken(), Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: int(kioskTTL.Seconds()),
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *app) kioskLocked() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return time.Now().Before(a.kioskLockUntil)
}

func (a *app) recordKioskAttempt(success bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if success {
		a.kioskFailCount = 0
		return
	}
	a.kioskFailCount++
	if a.kioskFailCount >= lockoutMax {
		a.kioskLockUntil = time.Now().Add(lockoutWindow)
		a.kioskFailCount = 0
	}
}
