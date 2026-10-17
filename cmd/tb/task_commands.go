package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

// SavedTask keeps argv separately so that quotes, spaces and empty arguments
// survive a save/run cycle. Shell tasks contain one unmodified command string.
type SavedTask struct {
	Key       string   `json:"key"`
	Name      string   `json:"name"`
	Directory string   `json:"directory"`
	Command   []string `json:"command"`
	Shell     string   `json:"shell,omitempty"`
}

var savedTaskKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
var pythonExecutablePattern = regexp.MustCompile(`^(python|pypy)([0-9]+(\.[0-9]+)*)?$`)

func commandBase(value string) string {
	value = strings.ReplaceAll(value, `\`, "/")
	return value[strings.LastIndex(value, "/")+1:]
}

func scriptTaskName(value string) string {
	base := commandBase(value)
	ext := strings.ToLower(filepath.Ext(base))
	switch ext {
	case ".py", ".pyw", ".sh", ".bash", ".zsh", ".js", ".cjs", ".mjs":
		base = base[:len(base)-len(ext)]
	}
	if base == "" || base == "." || base == ".." {
		return "task"
	}
	return base
}

func inferTaskName(command []string) string {
	if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
		return "task"
	}
	base := commandBase(command[0])
	executable := strings.TrimSuffix(strings.ToLower(base), ".exe")
	isPython := pythonExecutablePattern.MatchString(executable)
	isShell := executable == "sh" || executable == "bash" || executable == "dash" || executable == "zsh" || executable == "ksh" || executable == "fish"
	isNode := executable == "node" || executable == "nodejs"
	if !isPython && !isShell && !isNode {
		return base
	}
	for i := 1; i < len(command); i++ {
		arg := command[i]
		if arg == "--" {
			if i+1 < len(command) {
				return scriptTaskName(command[i+1])
			}
			break
		}
		if arg == "-" || arg == "" {
			break
		}
		if !strings.HasPrefix(arg, "-") {
			return scriptTaskName(arg)
		}
		if isPython {
			if arg == "-X" || arg == "-W" || arg == "--check-hash-based-pycs" {
				i++
				continue
			}
			if strings.HasPrefix(arg, "-X") || strings.HasPrefix(arg, "-W") || strings.HasPrefix(arg, "--check-hash-based-pycs=") {
				continue
			}
			if arg == "-m" {
				if i+1 < len(command) && strings.TrimSpace(command[i+1]) != "" {
					return command[i+1]
				}
				break
			}
			if strings.HasPrefix(arg, "-m") && len(arg) > 2 {
				return arg[2:]
			}
			if arg == "-c" || (!strings.HasPrefix(arg, "--") && strings.ContainsAny(arg, "cVh")) {
				break
			}
			if strings.HasPrefix(arg, "--") {
				break
			}
			if strings.TrimLeft(arg[1:], "bBdEiIOPqRsSuvVx") != "" {
				break
			}
		} else if isShell {
			if strings.HasPrefix(arg, "--") {
				if arg == "--login" || arg == "--noprofile" || arg == "--norc" {
					continue
				}
				break
			}
			if strings.Contains(arg, "c") {
				break
			}
			if arg == "-o" || arg == "-O" {
				i++
			}
		} else {
			switch arg {
			case "-e", "--eval", "-p", "--print", "-v", "--version", "-h", "--help":
				return executable
			case "-r", "--require", "--loader", "--experimental-loader", "--import", "--conditions", "-C":
				i++
			default:
				if strings.HasPrefix(arg, "--eval=") || strings.HasPrefix(arg, "--print=") || strings.HasPrefix(arg, "-e") || strings.HasPrefix(arg, "-p") {
					return executable
				}
			}
		}
	}
	return executable
}

// shellCommand runs user-entered shell text without rewriting quotes or lines.
// It intentionally adds neither history expansion nor shell aliases.
func shellCommand(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	if runtime.GOOS == "windows" {
		shell := os.Getenv("ComSpec")
		if shell == "" {
			shell = "cmd.exe"
		}
		return []string{shell, "/S", "/C", raw}
	}
	return []string{"/bin/sh", "-c", raw}
}

func savedTasksPath() string {
	return filepath.Join(filepath.Dir(configPath()), "tasks.json")
}

func validateSavedTask(task SavedTask) error {
	if !savedTaskKeyPattern.MatchString(task.Key) {
		return errors.New("task key must contain only letters, digits, _ or -")
	}
	if strings.TrimSpace(task.Name) == "" {
		return errors.New("task display name must not be empty")
	}
	if !filepath.IsAbs(task.Directory) {
		return errors.New("task directory must be an absolute path")
	}
	if len(task.Command) == 0 || strings.TrimSpace(task.Command[0]) == "" {
		return errors.New("task command must not be empty")
	}
	for _, arg := range task.Command {
		if strings.ContainsRune(arg, '\x00') {
			return errors.New("task command must not contain NUL bytes")
		}
	}
	if task.Shell != "" && len(task.Command) != 1 {
		return errors.New("shell task must contain one raw command string")
	}
	return nil
}

func loadSavedTasks() ([]SavedTask, error) {
	data, err := os.ReadFile(savedTasksPath())
	if os.IsNotExist(err) {
		return []SavedTask{}, nil
	}
	if err != nil {
		return nil, err
	}
	if trimmed := bytes.TrimSpace(data); len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, fmt.Errorf("cannot read saved tasks: expected a JSON array in %s", savedTasksPath())
	}
	var tasks []SavedTask
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&tasks); err != nil {
		return nil, fmt.Errorf("cannot read saved tasks: %w", err)
	}
	var extra any
	if err = decoder.Decode(&extra); err != io.EOF {
		return nil, errors.New("cannot read saved tasks: unexpected data after JSON array")
	}
	seen := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		if err = validateSavedTask(task); err != nil {
			return nil, fmt.Errorf("cannot read saved task %q: %w", task.Key, err)
		}
		if seen[task.Key] {
			return nil, fmt.Errorf("cannot read saved tasks: duplicate key %q", task.Key)
		}
		seen[task.Key] = true
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].Key < tasks[j].Key })
	return tasks, nil
}

func writeSavedTasks(tasks []SavedTask) error {
	directory := filepath.Dir(savedTasksPath())
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	if tasks == nil {
		tasks = []SavedTask{}
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].Key < tasks[j].Key })
	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".tasks-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if err = temporary.Chmod(0600); err == nil {
		_, err = temporary.Write(append(data, '\n'))
	}
	if err == nil {
		err = temporary.Sync()
	}
	closeErr := temporary.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(temporary.Name(), savedTasksPath())
}

func saveSavedTask(task SavedTask) error {
	if err := validateSavedTask(task); err != nil {
		return err
	}
	tasks, err := loadSavedTasks()
	if err != nil {
		return err
	}
	for index := range tasks {
		if tasks[index].Key == task.Key {
			tasks[index] = task
			return writeSavedTasks(tasks)
		}
	}
	return writeSavedTasks(append(tasks, task))
}

func deleteSavedTask(key string) error {
	if !savedTaskKeyPattern.MatchString(key) {
		return errors.New("task key must contain only letters, digits, _ or -")
	}
	tasks, err := loadSavedTasks()
	if err != nil {
		return err
	}
	for index := range tasks {
		if tasks[index].Key == key {
			return writeSavedTasks(append(tasks[:index], tasks[index+1:]...))
		}
	}
	return fmt.Errorf("saved task %q: %w", key, os.ErrNotExist)
}

// runSavedTask also accepts a task prepared by the menu but not yet saved.
// Key is used only for persistence; Shell denotes raw text, not an executable.
func runSavedTask(c Config, task SavedTask, extra []string, in io.Reader, out, errOut io.Writer) int {
	if len(task.Command) == 0 || strings.TrimSpace(task.Command[0]) == "" {
		fmt.Fprintln(errOut, "TaskBridge: task command must not be empty")
		return 2
	}
	command := append([]string(nil), task.Command...)
	if task.Shell != "" {
		if len(command) != 1 {
			fmt.Fprintln(errOut, "TaskBridge: shell task must contain one raw command string")
			return 2
		}
		if len(extra) > 0 {
			fmt.Fprintln(errOut, "TaskBridge: extra argv cannot be appended to a shell task; edit its command from the menu")
			return 2
		}
		command = shellCommand(task.Command[0])
	} else {
		command = append(command, extra...)
	}
	original, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(errOut, "TaskBridge:", err)
		return 1
	}
	// A relative TB_CONFIG can produce a relative queue path. Resolve it before
	// changing directory so retries continue using the same client queue.
	if c.QueueDir != "" {
		c.QueueDir, err = filepath.Abs(c.QueueDir)
		if err != nil {
			fmt.Fprintln(errOut, "TaskBridge:", err)
			return 1
		}
	}
	if err = os.Chdir(task.Directory); err != nil {
		fmt.Fprintln(errOut, "TaskBridge:", err)
		return 1
	}
	defer func() {
		if err := os.Chdir(original); err != nil {
			fmt.Fprintf(errOut, "TaskBridge: cannot restore working directory: %v\n", err)
		}
	}()
	return runTask(c, append([]string{"-n", task.Name, "--"}, command...), in, out, errOut)
}

func executeTasks(args []string, in io.Reader, out, errOut io.Writer) int {
	usage := func() int {
		fmt.Fprintln(errOut, "usage: tb tasks list | save NAME [-n DISPLAY] [--cwd DIR] -- command [args...] | run NAME [-- additional args...] | remove NAME")
		return 2
	}
	failed := func(err error) int {
		fmt.Fprintln(errOut, "TaskBridge:", err)
		return 1
	}
	if len(args) == 0 {
		args = []string{"list"}
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return usage()
		}
		tasks, err := loadSavedTasks()
		if err != nil {
			return failed(err)
		}
		if len(tasks) == 0 {
			fmt.Fprintln(out, "No saved tasks. Save one with: tb tasks save NAME -- command [args...]")
		}
		for _, task := range tasks {
			fmt.Fprintf(out, "%s\t%s\t%s\n", task.Key, task.Name, task.Directory)
		}
		return 0
	case "save":
		if len(args) < 3 {
			return usage()
		}
		flags := flag.NewFlagSet("tasks save", flag.ContinueOnError)
		flags.SetOutput(errOut)
		name := flags.String("n", "", "display name")
		directory := flags.String("cwd", "", "working directory (defaults to current directory)")
		if err := flags.Parse(args[2:]); err != nil {
			return 2
		}
		argv := flags.Args()
		if *name == "" {
			*name = inferTaskName(argv)
		}
		absolute, err := filepath.Abs(*directory)
		if err != nil {
			return failed(err)
		}
		task := SavedTask{Key: args[1], Name: *name, Directory: absolute, Command: argv}
		if err = validateSavedTask(task); err != nil {
			fmt.Fprintln(errOut, "TaskBridge:", err)
			return 2
		}
		info, err := os.Stat(absolute)
		if err != nil {
			return failed(err)
		}
		if !info.IsDir() {
			return failed(fmt.Errorf("%s is not a directory", absolute))
		}
		if err = saveSavedTask(task); err != nil {
			return failed(err)
		}
		fmt.Fprintf(out, "Saved %s (%s). Run with: tb tasks run %s\n", task.Key, task.Name, task.Key)
		return 0
	case "remove":
		if len(args) != 2 {
			return usage()
		}
		if err := deleteSavedTask(args[1]); err != nil {
			return failed(err)
		}
		fmt.Fprintf(out, "Removed saved task %s.\n", args[1])
		return 0
	case "run":
		if len(args) < 2 {
			return usage()
		}
		extra := args[2:]
		if len(extra) > 0 {
			if extra[0] != "--" {
				return usage()
			}
			extra = extra[1:]
		}
		tasks, err := loadSavedTasks()
		if err != nil {
			return failed(err)
		}
		for _, task := range tasks {
			if task.Key != args[1] {
				continue
			}
			config, err := loadConfig()
			if err != nil {
				fmt.Fprintf(errOut, "TaskBridge: run tb init first: %v\n", err)
				return 2
			}
			return runSavedTask(config, task, extra, in, out, errOut)
		}
		return failed(fmt.Errorf("saved task %q: %w", args[1], os.ErrNotExist))
	default:
		return usage()
	}
}
