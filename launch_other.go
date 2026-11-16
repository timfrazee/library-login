//go:build !windows

package main

func isDoubleClick() bool { return false }

func windowsLaunchMenu(dataDir string) launchMode { return launchUnset }

func firewallHint(port int) string { return "" }
