package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNonTTYEntryPointsRemainScriptFriendly(t *testing.T) {
	t.Setenv("TB_CONFIG", filepath.Join(t.TempDir(), "missing.json"))
	for _, tc := range []struct {
		args []string
		want int
	}{
		{nil, 2}, {[]string{"--help"}, 0}, {[]string{"menu"}, 2}, {[]string{"settings"}, 2},
	} {
		var out, errOut bytes.Buffer
		code := execute(tc.args, strings.NewReader(""), &out, &errOut)
		if code != tc.want || !strings.Contains(out.String()+errOut.String(), "tb") {
			t.Fatalf("args=%v code=%d out=%s err=%s", tc.args, code, out.String(), errOut.String())
		}
	}
}

func TestMenuNavigationAndCancel(t *testing.T) {
	m := selectionModel{title: "Menu", options: []string{"One", "Two", "Back"}, selected: 0, height: 24}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = next.(selectionModel)
	if m.selected != 2 {
		t.Fatal("up should wrap to last option")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(selectionModel)
	if !m.cancelled || m.accepted {
		t.Fatal("Esc should discard selection")
	}
}

func TestTerminalEOFKeyCancelsWithoutAccepting(t *testing.T) {
	levels, _ := alertPreset("standard")
	models := []tea.Model{
		selectionModel{options: []string{"Run", "Back"}},
		newSettingsModel(levels, ""),
		newEditModel("Command", "", "exit 99", true),
	}
	for _, model := range models {
		next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
		if cmd == nil {
			t.Fatalf("%T did not quit on Ctrl-D", model)
		}
		switch m := next.(type) {
		case selectionModel:
			if !m.cancelled || m.accepted {
				t.Fatal("EOF accepted selection")
			}
		case settingsModel:
			if !m.cancelled || m.accepted {
				t.Fatal("EOF saved settings")
			}
		case editModel:
			if !m.cancelled || m.accepted {
				t.Fatal("EOF submitted command")
			}
		}
	}
}

func TestSettingsAreOnlyReturnedOnExplicitSave(t *testing.T) {
	initial := map[string]int{"success": 3, "failure": 4, "question": 4, "interrupt": 4, "lost": 5, "manual": 3}
	m := newSettingsModel(initial, "")
	m.selected = 1 // successful completion
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = next.(settingsModel)
	if initial["success"] != 3 || m.levels["success"] != 2 {
		t.Fatal("draft must not mutate the original settings")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if next.(settingsModel).accepted {
		t.Fatal("cancel saved settings")
	}
	m = newSettingsModel(initial, "")
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft}) // preset left -> low noise
	m = next.(settingsModel)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(settingsModel)
	if !m.accepted || m.levels["success"] != 2 || m.levels["failure"] != 4 || m.levels["lost"] != 5 {
		t.Fatalf("wrong saved preset: %+v", m)
	}
}

func TestCommandEditorPreservesPastedQuotesAndLinesUntilConfirmed(t *testing.T) {
	raw := "CUDA_VISIBLE_DEVICES=0 python 'train mouth.py' \\\n  --output-dir '/tmp/new output'"
	m := newEditModel("Command", "", "", true)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(raw), Paste: true})
	m = next.(editModel)
	if string(m.text) != raw || m.accepted {
		t.Fatal("pasted newlines must not submit the editor")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !next.(editModel).accepted {
		t.Fatal("Enter should submit a complete pasted command")
	}
	m = newEditModel("Name", "", "训练", false)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = next.(editModel)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("嘴")})
	m = next.(editModel)
	if string(m.text) != "训嘴练" {
		t.Fatalf("unicode edit=%q", string(m.text))
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	if len(next.(editModel).text) != 0 {
		t.Fatal("Ctrl-U should clear text")
	}
}

func TestShellNameAndPreviewDoNotInterpretCommands(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"python3 'scripts/train mouth.py' --output x", "train mouth"},
		{"CUDA_VISIBLE_DEVICES=0 .venv/bin/python -u scripts/train.py", "train"},
		{"python -m project.train --gpus 0,1", "project.train"},
	} {
		if got := inferShellTaskName(tc.raw); got != tc.want {
			t.Fatalf("%q -> %q want %q", tc.raw, got, tc.want)
		}
	}
	if got := terminalText("name\x1b[2J\r\x00"); strings.ContainsAny(got, "\x1b\r\x00") {
		t.Fatalf("unsafe terminal display %q", got)
	}
	preview := displayTaskCommand(SavedTask{Command: []string{"python", "a b.py", "$LITERAL", ""}})
	if !strings.Contains(preview, "'a b.py'") || !strings.Contains(preview, "'$LITERAL'") || !strings.Contains(preview, "''") {
		t.Fatalf("lossy argv preview %s", preview)
	}
}

type scriptedScreens struct {
	choices []int
	edits   []string
	seen    []string
}

func (s *scriptedScreens) choose(title, detail string, options []string, selected int) (int, error) {
	s.seen = append(s.seen, title+"\n"+detail)
	if len(s.choices) == 0 {
		return 0, errUICancel
	}
	value := s.choices[0]
	s.choices = s.choices[1:]
	if value < 0 {
		return 0, errUICancel
	}
	return value, nil
}
func (s *scriptedScreens) edit(title, label, value string, multi bool) (string, error) {
	if len(s.edits) == 0 {
		return "", errUICancel
	}
	v := s.edits[0]
	s.edits = s.edits[1:]
	return v, nil
}
func (s *scriptedScreens) settings(initial map[string]int, detail string) (map[string]int, bool, error) {
	return nil, false, errUICancel
}

func TestTaskPreviewBackDoesNotLaunchOrSave(t *testing.T) {
	t.Setenv("TB_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	cwd, _ := os.Getwd()
	driver := &scriptedScreens{choices: []int{5}}
	var out, errOut bytes.Buffer
	task := SavedTask{Name: "train", Directory: cwd, Shell: "sh", Command: []string{"exit 99"}}
	code, ran, err := reviewTask(Config{}, task, false, strings.NewReader(""), &out, &errOut, driver)
	if err != nil || ran || code != 0 {
		t.Fatalf("cancel: code=%d ran=%v err=%v", code, ran, err)
	}
	if _, err := os.Stat(savedTasksPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("preview wrote favorites")
	}
	if len(driver.seen) != 1 || !strings.Contains(driver.seen[0], "exit 99") || !strings.Contains(driver.seen[0], cwd) {
		t.Fatal("preview omitted command/directory")
	}
}

func TestEditingArgvKeepsUnchangedLiteralArguments(t *testing.T) {
	args := []string{"python", "a b.py", "$LITERAL", ""}
	driver := &scriptedScreens{choices: []int{1, 0, 5}, edits: []string{"new script.py"}}
	got, err := editTaskCommand(SavedTask{Command: args}, driver)
	if err != nil || got.Shell != "" || !reflect.DeepEqual(got.Command, []string{"python", "new script.py", "$LITERAL", ""}) || args[1] != "a b.py" {
		t.Fatalf("argv edit=%+v err=%v", got, err)
	}
}
