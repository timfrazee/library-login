package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestKioskGate(t *testing.T) {
	a := testApp(t)
	a.tpl = themeTemplates(t)
	handler := a.routes()

	get := func(cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "192.168.1.50:1234"
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}

	unlock := func(code string) *httptest.ResponseRecorder {
		form := url.Values{"code": {code}}
		r := httptest.NewRequest(http.MethodPost, "/kiosk-unlock", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.RemoteAddr = "192.168.1.50:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}

	if !strings.Contains(get(nil).Body.String(), `name="id"`) {
		t.Fatal("with no code set, a LAN device should see the kiosk form")
	}

	if err := setConfig(a.db, cfgKioskAccessCode, "open-sesame"); err != nil {
		t.Fatalf("set code: %v", err)
	}

	gate := get(nil).Body.String()
	if strings.Contains(gate, `name="id"`) {
		t.Fatal("with a code set, an unpaired LAN device should be gated, not shown the kiosk")
	}
	if !strings.Contains(gate, `name="code"`) {
		t.Fatal("gate page should prompt for the access code")
	}

	if pairingCookie(unlock("wrong")) != nil {
		t.Fatal("a wrong code must not set a pairing cookie")
	}

	cookie := pairingCookie(unlock("open-sesame"))
	if cookie == nil {
		t.Fatal("the correct code should set a pairing cookie")
	}
	if !strings.Contains(get(cookie).Body.String(), `name="id"`) {
		t.Fatal("a paired device should reach the kiosk form")
	}

	if err := setConfig(a.db, cfgKioskAccessCode, "rotated"); err != nil {
		t.Fatalf("rotate code: %v", err)
	}
	if strings.Contains(get(cookie).Body.String(), `name="id"`) {
		t.Fatal("rotating the code should invalidate the old pairing cookie")
	}
}

func TestKioskGateLoopbackExempt(t *testing.T) {
	a := testApp(t)
	a.tpl = themeTemplates(t)
	if err := setConfig(a.db, cfgKioskAccessCode, "secret"); err != nil {
		t.Fatalf("set code: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:5000"
	w := httptest.NewRecorder()
	a.routes().ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), `name="id"`) {
		t.Fatal("loopback clients should bypass the gate")
	}
}

func pairingCookie(w *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == kioskCookie {
			return c
		}
	}
	return nil
}
