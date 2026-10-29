package main

import (
	"net"
	"os/exec"
	"runtime"
)

type platform struct {
	bindHost string
	openHost string
}

// detectPlatform distinguishes ChromeOS/Crostini from desktop OSes. Inside
// Crostini the browser runs outside the Linux container and reaches it only via
// penguin.linux.test, so the server must listen on all interfaces there.
func detectPlatform() platform {
	if _, err := net.LookupHost("penguin.linux.test"); err == nil {
		return platform{bindHost: "0.0.0.0", openHost: "penguin.linux.test"}
	}
	return platform{bindHost: "127.0.0.1", openHost: "localhost"}
}

func openBrowser(url string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{url}
	case "windows":
		name, args = "cmd", []string{"/c", "start", "", url}
	default:
		name, args = "xdg-open", []string{url}
	}
	return exec.Command(name, args...).Start()
}
