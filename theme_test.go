package main

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
)

func themeTemplates(t *testing.T) *template.Template {
	t.Helper()
	tpl, err := template.ParseFS(assetsFS, "assets/*.html", "assets/admin/*.html")
	if err != nil {
		t.Fatalf("parse templates: %v", err)
	}
	return tpl
}

func render(t *testing.T, tpl *template.Template, name string, data any) string {
	t.Helper()
	var b bytes.Buffer
	if err := tpl.ExecuteTemplate(&b, name, data); err != nil {
		t.Fatalf("execute %s: %v", name, err)
	}
	return b.String()
}

func TestKioskThemeAttributes(t *testing.T) {
	tpl := themeTemplates(t)

	dark := render(t, tpl, "kiosk.html", kioskView{ThemeMode: "dark", ThemeColor: "violet", InputLabel: "Scan your badge"})
	if !strings.Contains(dark, `data-theme="dark"`) || !strings.Contains(dark, `data-accent="violet"`) {
		t.Errorf("explicit mode missing attributes:\n%s", dark[:min(200, len(dark))])
	}
	if !strings.Contains(dark, `<label for="id">Scan your badge</label>`) {
		t.Error("kiosk should render the configured input label")
	}

	system := render(t, tpl, "kiosk.html", kioskView{ThemeMode: "", ThemeColor: "sky"})
	if strings.Contains(system, "data-theme") {
		t.Error("system mode should omit data-theme so the device preference wins")
	}
	if !strings.Contains(system, `data-accent="sky"`) {
		t.Error("system mode should still carry the accent")
	}
}

func TestSettingsAppearancePanel(t *testing.T) {
	tpl := themeTemplates(t)
	out := render(t, tpl, "settings.html", adminView{
		ThemeColor: "sky",
		Data: settingsData{
			ThemeMode:  "dark",
			ThemeColor: "emerald",
			Accents:    accents,
		},
	})
	if !strings.Contains(out, `value="dark" selected`) {
		t.Error("mode select should mark the stored mode selected")
	}
	if !strings.Contains(out, `data-accent="emerald"`) {
		t.Error("swatch for a palette color should render")
	}
	if !strings.Contains(out, `value="emerald" checked`) {
		t.Error("the stored accent swatch should be checked")
	}
}
