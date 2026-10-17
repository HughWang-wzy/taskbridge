//go:build !windows

package main

import "os/exec"

func newTaskCommand(argv []string) *exec.Cmd {
	return exec.Command(argv[0], argv[1:]...)
}
