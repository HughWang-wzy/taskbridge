package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

// The child is a copy of this test executable placed in a directory containing
// spaces. It reports what its own Windows argv parser actually received.
func TestWindowsTaskCommandHelper(t *testing.T) {
	if os.Getenv("TB_WINDOWS_COMMAND_HELPER") != "1" {
		return
	}
	for index, arg := range os.Args {
		if arg != "--" {
			continue
		}
		if err := json.NewEncoder(os.Stdout).Encode(os.Args[index+1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(98)
		}
		exit, err := strconv.Atoi(os.Getenv("TB_WINDOWS_COMMAND_EXIT"))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(99)
		}
		os.Exit(exit)
	}
	fmt.Fprintln(os.Stderr, "helper did not receive an argument separator")
	os.Exit(97)
}

func TestNewTaskCommandWindowsPreservesRawShellQuotingAndExitCode(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "directory with spaces")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	helperPath := filepath.Join(directory, "argv helper.exe")
	destination, err := os.OpenFile(helperPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(destination, source)
	closeErr := destination.Close()
	if copyErr != nil {
		t.Fatal(copyErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}

	// These quotes are intentional Windows shell input. Building the expectation
	// independently catches cmd.exe quoting that escapes or drops the raw text.
	raw := `"` + helperPath + `" -test.run=TestWindowsTaskCommandHelper -- "alpha beta" "say \"hello world\"" "a & b" "C:\data folder\run" ""`
	wantArguments := []string{"alpha beta", `say "hello world"`, "a & b", `C:\data folder\run`, ""}
	for _, wantExit := range []int{0, 7} {
		t.Run(fmt.Sprintf("exit_%d", wantExit), func(t *testing.T) {
			command := newTaskCommand(shellCommand(raw))
			command.Env = append(os.Environ(), "TB_WINDOWS_COMMAND_HELPER=1", "TB_WINDOWS_COMMAND_EXIT="+strconv.Itoa(wantExit))
			var out, errOut bytes.Buffer
			command.Stdout, command.Stderr = &out, &errOut
			runErr := command.Run()
			gotExit := 0
			if runErr != nil {
				var exitErr *exec.ExitError
				if !errors.As(runErr, &exitErr) {
					t.Fatalf("could not start shell: %v; stderr=%s", runErr, &errOut)
				}
				gotExit = exitErr.ExitCode()
			}
			if gotExit != wantExit {
				t.Fatalf("exit=%d want=%d; stdout=%q stderr=%q", gotExit, wantExit, out.String(), errOut.String())
			}
			var gotArguments []string
			if err := json.Unmarshal(out.Bytes(), &gotArguments); err != nil {
				t.Fatalf("invalid child output: %v; stdout=%q stderr=%q", err, out.String(), errOut.String())
			}
			if !reflect.DeepEqual(gotArguments, wantArguments) {
				t.Fatalf("argv=%q want=%q; stderr=%q", gotArguments, wantArguments, errOut.String())
			}
		})
	}
}
