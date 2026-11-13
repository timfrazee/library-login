package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// installDesktopLauncher registers the app in the Linux/ChromeOS application
// launcher, pointing at the binary's current path so it survives being moved.
// Terminal=true keeps the "leave this running" window visible and closable. It
// is a best-effort no-op on other platforms and on any error.
func installDesktopLauncher(exePath string) {
	if runtime.GOOS != "linux" {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(home, ".local", "share", "applications")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	entry := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=Library Login
Comment=Entry/exit logging kiosk
Exec=%s
Terminal=true
Categories=Utility;
`, exePath)
	_ = os.WriteFile(filepath.Join(dir, "library-login.desktop"), []byte(entry), 0o644)
}
