package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	sessionCookie  = "ll_session"
	sessionTTL     = 8 * time.Hour
	lockoutMax     = 5
	lockoutWindow  = time.Minute
	minPasswordLen = 8
)

func (a *app) passwordSet() bool {
	_, ok, _ := getConfig(a.db, cfgAdminHash)
	return ok
}

func (a *app) setPassword(pw string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return setConfig(a.db, cfgAdminHash, string(hash))
}

func (a *app) checkPassword(pw string) bool {
	hash, ok, err := getConfig(a.db, cfgAdminHash)
	if err != nil || !ok {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// secret returns the HMAC key for session/CSRF signing, generating and
// persisting one on first use so tokens survive restarts.
func (a *app) secret() []byte {
	if s := configOr(a.db, cfgSessionSecret, ""); s != "" {
		return []byte(s)
	}
	s := randomHex(32)
	_ = setConfig(a.db, cfgSessionSecret, s)
	return []byte(s)
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (a *app) sign(msg string) string {
	mac := hmac.New(sha256.New, a.secret())
	mac.Write([]byte(msg))
	return hex.EncodeToString(mac.Sum(nil))
}

// tokenForNonce builds a session token carrying a fresh expiry for an existing
// nonce. The nonce stays stable across a session so the CSRF token derived from
// it does too; only the expiry slides forward on activity.
func (a *app) tokenForNonce(nonce string) string {
	payload := nonce + "." + strconv.FormatInt(time.Now().Add(sessionTTL).Unix(), 10)
	return payload + "." + a.sign(payload)
}

func (a *app) newSession() string { return a.tokenForNonce(randomHex(16)) }

// authSession validates the session cookie and returns its nonce. The nonce is
// stable for the session's life, so the CSRF token derived from it is too.
func (a *app) authSession(r *http.Request) (nonce string, ok bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return "", false
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 3 {
		return "", false
	}
	payload := parts[0] + "." + parts[1]
	if subtle.ConstantTimeCompare([]byte(parts[2]), []byte(a.sign(payload))) != 1 {
		return "", false
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().Unix() >= exp {
		return "", false
	}
	return parts[0], true
}

func (a *app) setSession(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/admin/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: int(sessionTTL.Seconds()),
	})
}

func (a *app) clearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/admin/", HttpOnly: true, MaxAge: -1,
	})
}

func (a *app) csrfToken(nonce string) string { return a.sign("csrf." + nonce) }

func (a *app) checkCSRF(r *http.Request) bool {
	nonce, ok := a.authSession(r)
	if !ok {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(r.FormValue("csrf")), []byte(a.csrfToken(nonce))) == 1
}

// requireAuth gates admin pages: unconfigured installs go to setup, unauthenticated
// requests to login. Each authorized request slides the session expiry forward.
func (a *app) requireAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.passwordSet() {
			http.Redirect(w, r, "/admin/setup", http.StatusSeeOther)
			return
		}
		nonce, ok := a.authSession(r)
		if !ok {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		a.setSession(w, a.tokenForNonce(nonce))
		h(w, r)
	}
}

func (a *app) locked() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return time.Now().Before(a.lockUntil)
}

func (a *app) recordLoginResult(success bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if success {
		a.failCount = 0
		return
	}
	a.failCount++
	if a.failCount >= lockoutMax {
		a.lockUntil = time.Now().Add(lockoutWindow)
		a.failCount = 0
	}
}

func (a *app) handleSetupForm(w http.ResponseWriter, r *http.Request) {
	if a.passwordSet() {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}
	a.render(w, "setup.html", adminView{
		SiteName:   a.siteName(),
		FlashErr:   r.URL.Query().Get("err"),
		ThemeMode:  themeModeAttr(a.themeMode()),
		ThemeColor: a.themeColor(),
	})
}

func (a *app) handleSetupSubmit(w http.ResponseWriter, r *http.Request) {
	if a.passwordSet() {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}
	pw, confirm := r.FormValue("password"), r.FormValue("confirm")
	if err := validatePassword(pw, confirm); err != nil {
		http.Redirect(w, r, "/admin/setup?err="+err.Error(), http.StatusSeeOther)
		return
	}
	if err := a.setPassword(pw); err != nil {
		http.Error(w, "could not save password", http.StatusInternalServerError)
		return
	}
	a.setSession(w, a.newSession())
	http.Redirect(w, r, "/admin/", http.StatusSeeOther)
}

func (a *app) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	if !a.passwordSet() {
		http.Redirect(w, r, "/admin/setup", http.StatusSeeOther)
		return
	}
	if _, ok := a.authSession(r); ok {
		http.Redirect(w, r, "/admin/", http.StatusSeeOther)
		return
	}
	a.render(w, "login.html", adminView{
		SiteName:   a.siteName(),
		FlashErr:   r.URL.Query().Get("err"),
		ThemeMode:  themeModeAttr(a.themeMode()),
		ThemeColor: a.themeColor(),
	})
}

func (a *app) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	if a.locked() {
		http.Redirect(w, r, "/admin/login?err=Too+many+attempts.+Try+again+shortly.", http.StatusSeeOther)
		return
	}
	if !a.checkPassword(r.FormValue("password")) {
		a.recordLoginResult(false)
		http.Redirect(w, r, "/admin/login?err=Incorrect+password", http.StatusSeeOther)
		return
	}
	a.recordLoginResult(true)
	a.setSession(w, a.newSession())
	http.Redirect(w, r, "/admin/", http.StatusSeeOther)
}

func (a *app) handleLogout(w http.ResponseWriter, r *http.Request) {
	if !a.checkCSRF(r) {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	a.clearSession(w)
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

func validatePassword(pw, confirm string) error {
	if len(pw) < minPasswordLen {
		return errors.New("Password+must+be+at+least+8+characters")
	}
	if pw != confirm {
		return errors.New("Passwords+do+not+match")
	}
	return nil
}
