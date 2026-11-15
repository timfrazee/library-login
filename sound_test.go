package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidAudio(t *testing.T) {
	cases := []struct {
		ext  string
		data []byte
		want bool
	}{
		{".wav", []byte("RIFF\x00\x00\x00\x00WAVE"), true},
		{".wav", []byte("RIFFxxxxMP3 "), false},
		{".ogg", []byte("OggS\x00\x00"), true},
		{".ogg", []byte("NOPE"), false},
		{".mp3", []byte("ID3\x04"), true},
		{".mp3", []byte{0xFF, 0xFB, 0x90}, true},
		{".mp3", []byte("xxxx"), false},
		{".flac", []byte("fLaC"), false},
	}
	for _, c := range cases {
		if got := validAudio(c.ext, c.data); got != c.want {
			t.Errorf("validAudio(%q, %q) = %v, want %v", c.ext, c.data, got, c.want)
		}
	}
}

func TestKioskSoundMarkup(t *testing.T) {
	tpl := themeTemplates(t)

	on := render(t, tpl, "kiosk.html", kioskView{SoundEnabled: true})
	if !strings.Contains(on, "new Audio('/sound/login')") {
		t.Error("enabled kiosk should wire up the sound elements")
	}

	off := render(t, tpl, "kiosk.html", kioskView{})
	if strings.Contains(off, "new Audio(") {
		t.Error("disabled kiosk should emit no audio")
	}
}

func TestSettingsAudioPanel(t *testing.T) {
	tpl := themeTemplates(t)
	out := render(t, tpl, "settings.html", adminView{Data: settingsData{
		SoundEnabled: true,
		Sounds: []soundSetting{
			{Event: "login", Label: "Login", Custom: true},
			{Event: "logout", Label: "Logout", Custom: false},
		},
	}})
	if !strings.Contains(out, `name="kiosk_sound" checked`) {
		t.Error("audio toggle should reflect the enabled state")
	}
	if !strings.Contains(out, `src="/sound/login"`) || !strings.Contains(out, `name="delete_sound_login"`) {
		t.Error("audio panel should preview a sound and offer removal of a custom file")
	}
	if !strings.Contains(out, `class="file-input is-custom"`) || !strings.Contains(out, `>Custom</span>`) {
		t.Error("a customized sound should show the custom state")
	}
	if !strings.Contains(out, `>Default</span>`) {
		t.Error("an uncustomized sound should show the default state")
	}
}

func TestHandleSound(t *testing.T) {
	a := &app{dataDir: t.TempDir()}

	serve := func(event string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/sound/"+event, nil)
		req.SetPathValue("event", event)
		rec := httptest.NewRecorder()
		a.handleSound(rec, req)
		return rec
	}

	rec := serve("login")
	if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
		t.Fatalf("default login sound: code %d, %d bytes", rec.Code, rec.Body.Len())
	}

	if rec := serve("bogus"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown event: code %d, want 404", rec.Code)
	}

	custom := []byte("OggS custom audio")
	if err := os.WriteFile(filepath.Join(a.dataDir, "error.ogg"), custom, 0o644); err != nil {
		t.Fatal(err)
	}
	if rec := serve("error"); rec.Body.String() != string(custom) {
		t.Errorf("custom sound not served: got %q", rec.Body.String())
	}
}
