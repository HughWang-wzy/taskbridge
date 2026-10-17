package main

import (
	"os/exec"
	"strings"
	"syscall"
)

func newTaskCommand(argv []string) *exec.Cmd {
	cmd := exec.Command(argv[0], argv[1:]...)
	if strings.EqualFold(commandBase(argv[0]), "cmd.exe") && len(argv) == 4 && strings.EqualFold(argv[1], "/S") && strings.EqualFold(argv[2], "/C") {
		// cmd.exe parses a raw /C string rather than CommandLineToArgvW. Wrap
		// the original text once; do not backslash-escape its nested quotes.
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: syscall.EscapeArg(cmd.Path) + " /S /C \"" + argv[3] + "\""}
	}
	return cmd
}
