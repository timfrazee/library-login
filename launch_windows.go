//go:build windows

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	launchPrefFile  = "windows-launch-mode"
	launchCountdown = 10
)

var (
	kernel32                  = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
)

// isDoubleClick reports whether this process is the only one attached to its
// console. GetConsoleProcessList returns the count of attached processes; a
// result of 1 means Windows allocated a fresh console for a double-click rather
// than the process inheriting an existing shell.
func isDoubleClick() bool {
	var pids [2]uint32
	n, _, _ := procGetConsoleProcessList.Call(
		uintptr(unsafe.Pointer(&pids[0])),
		uintptr(len(pids)),
	)
	return n == 1
}

// windowsLaunchMenu asks whether to serve the whole network or only this
// computer, remembering the answer. The remembered choice auto-applies after a
// countdown, but the first-ever launch waits indefinitely for an explicit pick.
func windowsLaunchMenu(dataDir string) launchMode {
	def, hasDefault := loadLaunchPref(dataDir)

	fmt.Println()
	fmt.Println("How should Library Login run?")
	fmt.Println("  [1] Network - other devices (Chromebooks, tablets) can connect")
	fmt.Println("  [2] Local   - only this computer")
	fmt.Println()

	choice := promptLaunchMode(def, hasDefault)
	saveLaunchPref(dataDir, choice)
	return choice
}

func promptLaunchMode(def launchMode, hasDefault bool) launchMode {
	lines := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()

	if !hasDefault {
		fmt.Print("Press 1 or 2, then Enter: ")
		for line := range lines {
			if m, ok := parseLaunchChoice(line, launchUnset, false); ok {
				return m
			}
			fmt.Print("Please type 1 or 2, then Enter: ")
		}
		return launchNetwork
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	remaining := launchCountdown
	printCountdown(def, remaining)
	for {
		select {
		case line := <-lines:
			if m, ok := parseLaunchChoice(line, def, true); ok {
				fmt.Println()
				return m
			}
			printCountdown(def, remaining)
		case <-ticker.C:
			remaining--
			if remaining <= 0 {
				fmt.Println()
				return def
			}
			printCountdown(def, remaining)
		}
	}
}

func parseLaunchChoice(line string, def launchMode, allowDefault bool) (launchMode, bool) {
	switch strings.TrimSpace(line) {
	case "1":
		return launchNetwork, true
	case "2":
		return launchLocal, true
	case "":
		if allowDefault {
			return def, true
		}
	}
	return launchUnset, false
}

func printCountdown(def launchMode, remaining int) {
	name := "Network"
	if def == launchLocal {
		name = "Local"
	}
	fmt.Printf("\rStarting %s in %2ds - press 1, 2, or Enter to choose: ", name, remaining)
}

func loadLaunchPref(dataDir string) (launchMode, bool) {
	b, err := os.ReadFile(filepath.Join(dataDir, launchPrefFile))
	if err != nil {
		return launchUnset, false
	}
	switch strings.TrimSpace(string(b)) {
	case "network":
		return launchNetwork, true
	case "local":
		return launchLocal, true
	}
	return launchUnset, false
}

func saveLaunchPref(dataDir string, m launchMode) {
	s := "network"
	if m == launchLocal {
		s = "local"
	}
	_ = os.WriteFile(filepath.Join(dataDir, launchPrefFile), []byte(s+"\n"), 0o644)
}
