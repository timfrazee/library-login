package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
)

type launchMode int

const (
	launchUnset launchMode = iota
	launchNetwork
	launchLocal
)

// doubleClickLaunch records whether this process owns a freshly-spawned console
// (double-clicked in Explorer). When true, fatal startup errors pause so the
// window does not vanish before the message can be read.
var doubleClickLaunch bool

// explicitMode reports whether the user already pinned LAN-vs-local via a flag
// or environment variable, in which case the Windows launch menu must not ask.
func explicitMode(setFlags map[string]bool) bool {
	if setFlags["lan"] || setFlags["host"] {
		return true
	}
	return os.Getenv(envLan) != "" || os.Getenv(envHost) != ""
}

func fatalf(format string, args ...any) {
	log.Printf(format, args...)
	if doubleClickLaunch {
		pauseForExit()
	}
	os.Exit(1)
}

func pauseForExit() {
	fmt.Fprint(os.Stderr, "\nPress Enter to close this window...")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}
