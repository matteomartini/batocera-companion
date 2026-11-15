//go:build !windows

package server

import "os/exec"

func hideWindow(cmd *exec.Cmd) {}

func openPath(p string) error { return exec.Command("xdg-open", p).Start() }
func openURL(u string) error  { return exec.Command("xdg-open", u).Start() }
