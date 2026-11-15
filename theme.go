package main

// accent is one selectable accent color. Each Key has a matching
// [data-accent="Key"] block in style.css, and Hex is shown on the admin swatch.
type accent struct {
	Key   string
	Label string
	Hex   string
}

// accents is the curated palette offered in admin, in display order.
var accents = []accent{
	{"sky", "Sky", "#0284c7"},
	{"blue", "Blue", "#2563eb"},
	{"violet", "Violet", "#7c3aed"},
	{"emerald", "Emerald", "#059669"},
	{"teal", "Teal", "#0d9488"},
	{"amber", "Amber", "#d97706"},
	{"rose", "Rose", "#e11d48"},
}

const (
	themeModeSystem   = "system"
	themeColorDefault = "sky"
)

var themeModes = map[string]bool{themeModeSystem: true, "light": true, "dark": true}

func validAccent(key string) bool {
	for _, a := range accents {
		if a.Key == key {
			return true
		}
	}
	return false
}

func (a *app) themeMode() string {
	m := configOr(a.db, cfgThemeMode, themeModeSystem)
	if !themeModes[m] {
		return themeModeSystem
	}
	return m
}

func (a *app) themeColor() string {
	c := configOr(a.db, cfgThemeColor, themeColorDefault)
	if !validAccent(c) {
		return themeColorDefault
	}
	return c
}

// themeModeAttr maps the stored mode to the data-theme attribute value. System
// returns empty so the stylesheet falls through to the device's own light/dark
// preference via prefers-color-scheme.
func themeModeAttr(mode string) string {
	if mode == themeModeSystem {
		return ""
	}
	return mode
}
