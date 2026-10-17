package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestInferTaskNameFindsScriptWithoutTreatingCodeAsFilename(t *testing.T) {
	for _, tc := range []struct {
		command []string
		want    string
	}{
		{nil, "task"},
		{[]string{".venv/bin/python", "scripts/run_mouth_parallel.py", "--gpus", "0,1"}, "run_mouth_parallel"},
		{[]string{"python3.12", "-u", "-X", "dev", "-Wignore", "train.py"}, "train"},
		{[]string{"python3", "-Werror::DeprecationWarning", "-Xpycache_prefix=/tmp/cache", "train.py"}, "train"},
		{[]string{"python", "--check-hash-based-pycs", "always", "train.py"}, "train"},
		{[]string{"python", "-V", "train.py"}, "python"},
		{[]string{"python", "-m", "torch.distributed.run", "train.py"}, "torch.distributed.run"},
		{[]string{"python", "-c", "print('hello')"}, "python"},
		{[]string{"python3", "-uc", "print('hello')"}, "python3"},
		{[]string{"python", "-X"}, "python"},
		{[]string{"bash", "-e", "scripts/train.sh"}, "train"},
		{[]string{"sh", "-c", "echo result"}, "sh"},
		{[]string{"node", "--require", "setup.cjs", "scripts/serve.mjs"}, "serve"},
		{[]string{"node", "--eval", "console.log('x')"}, "node"},
		{[]string{"/usr/bin/sleep", "70"}, "sleep"},
		{[]string{`C:\Python312\python.exe`, `scripts\train.py`}, "train"},
	} {
		if got := inferTaskName(tc.command); got != tc.want {
			t.Errorf("inferTaskName(%q)=%q want %q", tc.command, got, tc.want)
		}
	}
}

func TestSavedTasksPreserveArgumentsAndReplaceByKey(t *testing.T) {
	t.Setenv("TB_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	command := []string{"python", "a script.py", "", "--name", "literal $(touch nope)"}
	want := SavedTask{Key: "training_1", Name: "模型训练", Directory: t.TempDir(), Command: command}
	if err := saveSavedTask(want); err != nil {
		t.Fatal(err)
	}
	want.Name = "训练续跑"
	if err := saveSavedTask(want); err != nil {
		t.Fatal(err)
	}
	got, err := loadSavedTasks()
	if err != nil || len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Fatalf("tasks=%+v error=%v want=%+v", got, err, want)
	}
	info, err := os.Stat(filepath.Join(filepath.Dir(configPath()), "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("permissions=%o", info.Mode().Perm())
	}
	if err := deleteSavedTask(want.Key); err != nil {
		t.Fatal(err)
	}
	got, err = loadSavedTasks()
	if err != nil || len(got) != 0 {
		t.Fatalf("tasks=%+v error=%v", got, err)
	}
}

func TestSavedTasksRejectInvalidAndCorruptDataWithoutOverwrite(t *testing.T) {
	t.Setenv("TB_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	valid := SavedTask{Key: "train", Name: "train", Directory: t.TempDir(), Command: []string{"python", "train.py"}}
	for _, key := range []string{"", "../escape", "train task", "train/name"} {
		task := valid
		task.Key = key
		if err := saveSavedTask(task); err == nil {
			t.Fatalf("accepted invalid key %q", key)
		}
	}
	path := filepath.Join(filepath.Dir(configPath()), "tasks.json")
	for _, contents := range []string{
		`null`,
		`[] {}`,
		`[{"key":"x","name":"x","directory":"/tmp","command":[]}]`,
		`[{"key":"x","name":"x","directory":"/tmp","command":["echo"],"surprise":true}]`,
		`[{"key":"x","name":"x","directory":"/tmp","command":["echo"]},{"key":"x","name":"x","directory":"/tmp","command":["echo"]}]`,
	} {
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadSavedTasks(); err == nil {
			t.Errorf("accepted corrupt data %q", contents)
		}
		if err := saveSavedTask(valid); err == nil {
			t.Errorf("overwrote corrupt data %q", contents)
		}
		got, _ := os.ReadFile(path)
		if string(got) != contents {
			t.Fatalf("corrupt file was changed: %q", got)
		}
	}
}

func TestShellCommandPreservesShellSyntaxAndRejectsEmpty(t *testing.T) {
	if got := shellCommand(" \n\t"); len(got) != 0 {
		t.Fatalf("accepted empty shell command: %q", got)
	}
	if runtime.GOOS == "windows" {
		t.Setenv("ComSpec", `C:\Windows\System32\cmd.exe`)
		got := shellCommand("echo hello & echo world")
		want := []string{`C:\Windows\System32\cmd.exe`, "/S", "/C", "echo hello & echo world"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("command=%q want=%q", got, want)
		}
		return
	}
	argv := shellCommand("printf '%s\\n' 'a b'\nprintf '%s' '$literal'")
	out, err := exec.Command(argv[0], argv[1:]...).Output()
	if err != nil || string(out) != "a b\n$literal" {
		t.Fatalf("output=%q error=%v", out, err)
	}
}

func TestTasksCLISavesExactArgvAndListsWithoutConfig(t *testing.T) {
	t.Setenv("TB_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	directory := t.TempDir()
	var out, errOut bytes.Buffer
	args := []string{"save", "train", "-n", "训练", "--cwd", directory, "--", "python", "a script.py", "--message", "space and $literal"}
	if code := executeTasks(args, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("code=%d errors=%s", code, &errOut)
	}
	tasks, err := loadSavedTasks()
	if err != nil || len(tasks) != 1 || !reflect.DeepEqual(tasks[0].Command, []string{"python", "a script.py", "--message", "space and $literal"}) || tasks[0].Directory != directory || tasks[0].Name != "训练" {
		t.Fatalf("tasks=%+v error=%v", tasks, err)
	}
	out.Reset()
	if code := executeTasks([]string{"list"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "训练") || !strings.Contains(out.String(), "train") {
		t.Fatalf("list code=%d output=%s errors=%s", code, &out, &errOut)
	}
	if code := executeTasks([]string{"remove", "train"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("remove code=%d errors=%s", code, &errOut)
	}
	if code := executeTasks([]string{"save", "bad", "--", ""}, strings.NewReader(""), &out, &errOut); code != 2 {
		t.Fatalf("invalid save code=%d errors=%s", code, &errOut)
	}
}

func TestTasksRunRestoresDirectoryAndPreservesArgumentsExitAndOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX shell for an argv and cwd probe")
	}
	t.Setenv("TB_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	var start map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/start") {
			_ = json.NewDecoder(r.Body).Decode(&start)
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	if err := saveConfig(Config{URL: server.URL, Token: "token", NtfyURL: server.URL, NtfyTopic: "topic"}); err != nil {
		t.Fatal(err)
	}
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	directory, err = filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveSavedTask(SavedTask{Key: "probe", Name: "目录验证", Directory: directory, Command: []string{"sh", "-c", `printf '%s\n%s\n%s' "$PWD" "$1" "$2"; exit 7`, "probe", "space and $literal"}}); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := executeTasks([]string{"run", "probe", "--", "tail ; not shell"}, strings.NewReader(""), &out, &errOut)
	if code != 7 || out.String() != directory+"\nspace and $literal\ntail ; not shell" {
		t.Fatalf("code=%d output=%q errors=%s", code, out.String(), &errOut)
	}
	if current, _ := os.Getwd(); current != original {
		t.Fatalf("directory=%s want=%s", current, original)
	}
	if start["name"] != "目录验证" || start["cwd"] != directory {
		t.Fatalf("start=%+v", start)
	}
}

func TestRunSavedTaskForwardsStdinAndRawShellWithoutRewriting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX shell to inspect stdin")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	config := Config{URL: server.URL, Token: "token", QueueDir: t.TempDir(), NtfyURL: server.URL, NtfyTopic: "topic"}
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	raw := "IFS= read -r line\nprintf '%s\\n%s' \"$line\" '$literal with spaces'"
	task := SavedTask{Name: "stdin check", Directory: directory, Shell: "sh", Command: []string{raw}}
	var out, errOut bytes.Buffer
	code := runSavedTask(config, task, nil, strings.NewReader("hello from stdin\n"), &out, &errOut)
	if code != 0 || out.String() != "hello from stdin\n$literal with spaces" {
		t.Fatalf("code=%d output=%q errors=%s", code, out.String(), &errOut)
	}
	if task.Command[0] != raw {
		t.Fatal("raw shell text was changed")
	}
	if current, _ := os.Getwd(); current != original {
		t.Fatalf("directory=%s want=%s", current, original)
	}
}

func TestRunSavedTaskRejectsMissingDirectoryBeforeStarting(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	task := SavedTask{Name: "missing", Directory: filepath.Join(t.TempDir(), "missing"), Command: []string{"not-started"}}
	var out, errOut bytes.Buffer
	code := runSavedTask(Config{URL: server.URL, Token: "token", QueueDir: t.TempDir()}, task, nil, strings.NewReader(""), &out, &errOut)
	if code == 0 || calls != 0 || errOut.Len() == 0 {
		t.Fatalf("code=%d calls=%d output=%q errors=%s", code, calls, out.String(), &errOut)
	}
	if current, _ := os.Getwd(); current != original {
		t.Fatalf("directory=%s want=%s", current, original)
	}
}

func TestRunSavedTaskRejectsEmptyOrAmbiguousShellCommand(t *testing.T) {
	for _, tc := range []struct {
		task  SavedTask
		extra []string
	}{
		{SavedTask{Command: nil}, nil},
		{SavedTask{Command: []string{" \n"}, Shell: "sh"}, nil},
		{SavedTask{Command: []string{"echo one", "echo two"}, Shell: "sh"}, nil},
		{SavedTask{Command: []string{"echo one"}, Shell: "sh"}, []string{"two"}},
	} {
		var out, errOut bytes.Buffer
		if code := runSavedTask(Config{}, tc.task, tc.extra, strings.NewReader(""), &out, &errOut); code != 2 {
			t.Fatalf("code=%d for task=%+v extra=%q errors=%s", code, tc.task, tc.extra, &errOut)
		}
	}
}
