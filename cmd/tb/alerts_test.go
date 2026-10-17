package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAlertsSavesBothClientConfigsAndRejectsInvalidLevels(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)
	t.Setenv("TB_CONFIG", "")
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	configDir = filepath.Join(configDir, "taskbridge")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config.json", "codex.json"} {
		if err := os.WriteFile(filepath.Join(configDir, name), []byte(`{"url":"https://worker.example","token":"keep-secret","codex_stop_title":"Keep title","codex_final_output":true}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var out, errOut bytes.Buffer
	if code := execute([]string{"alerts", "--success", "silent", "--failure", "urgent", "--question", "4", "--interrupt", "low", "--lost", "max"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("code=%d errors=%s", code, errOut.String())
	}
	for _, name := range []string{"config.json", "codex.json"} {
		path := filepath.Join(configDir, name)
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var cfg struct {
			Token      string         `json:"token"`
			Title      string         `json:"codex_stop_title"`
			Output     bool           `json:"codex_final_output"`
			Priorities map[string]int `json:"alert_priorities"`
		}
		if err := json.Unmarshal(before, &cfg); err != nil {
			t.Fatal(err)
		}
		if cfg.Token != "keep-secret" || cfg.Title != "Keep title" || !cfg.Output || cfg.Priorities["success"] != 1 || cfg.Priorities["failure"] != 5 || cfg.Priorities["question"] != 4 || cfg.Priorities["interrupt"] != 2 || cfg.Priorities["lost"] != 5 {
			t.Fatalf("%s: unexpected config fields", name)
		}
		out.Reset()
		errOut.Reset()
		if code := execute([]string{"alerts", "--success", "low", "--failure", "6"}, strings.NewReader(""), &out, &errOut); code != 2 {
			t.Fatalf("invalid code=%d errors=%s", code, errOut.String())
		}
		after, _ := os.ReadFile(path)
		if !bytes.Equal(before, after) {
			t.Fatalf("invalid input changed %s", name)
		}
	}
	out.Reset()
	errOut.Reset()
	if code := execute([]string{"alerts"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "success") || strings.Contains(out.String(), "keep-secret") {
		t.Fatalf("show code=%d output=%s errors=%s", code, out.String(), errOut.String())
	}
}

func TestNotifyPriorityAliasesAndValidation(t *testing.T) {
	for _, tc := range []struct {
		level      string
		want, code int
	}{
		{"", 4, 0}, {"silent", 1, 0}, {"MIN", 1, 0}, {"low", 2, 0}, {"default", 3, 0}, {"normal", 3, 0}, {"high", 4, 0}, {"urgent", 5, 0}, {"max", 5, 0}, {"1", 1, 0}, {"5", 5, 0}, {"0", 0, 2}, {"6", 0, 2}, {"2.5", 0, 2}, {"banana", 0, 2},
	} {
		t.Run(tc.level, func(t *testing.T) {
			var priorities []int
			ntfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Priority int `json:"priority"`
				}
				json.NewDecoder(r.Body).Decode(&body)
				priorities = append(priorities, body.Priority)
				w.WriteHeader(200)
			}))
			defer ntfy.Close()
			t.Setenv("TB_CONFIG", filepath.Join(t.TempDir(), "config.json"))
			if err := saveConfig(Config{URL: "http://127.0.0.1:1", NtfyURL: ntfy.URL, NtfyTopic: "topic", AlertPriorities: map[string]int{"manual": 4}}); err != nil {
				t.Fatal(err)
			}
			var out, errOut bytes.Buffer
			args := []string{"notify", "--title", "Test"}
			if tc.level != "" {
				args = append(args, "-p", tc.level)
			}
			args = append(args, "Body")
			code := execute(args, strings.NewReader(""), &out, &errOut)
			if code != tc.code || (tc.code == 0 && (len(priorities) != 1 || priorities[0] != tc.want)) || (tc.code != 0 && len(priorities) != 0) {
				t.Fatalf("code=%d sent=%v errors=%s", code, priorities, errOut.String())
			}
		})
	}
}

func TestRunSelectsOutcomePriorityAndRejectsBadPriorityBeforeExecution(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell syntax differs")
	}
	for _, tc := range []struct {
		name           string
		command, flags []string
		priority, code int
	}{
		{"success", []string{"sh", "-c", "exit 0"}, []string{"-p", "low", "--lost-priority", "high"}, 2, 0},
		{"failure", []string{"sh", "-c", "exit 7"}, []string{"-p", "low", "--failure-priority", "urgent"}, 5, 7},
		{"start failure", []string{"/nonexistent-taskbridge-test-command"}, []string{"--failure-priority", "min"}, 1, 127},
		{"configured success", []string{"sh", "-c", "exit 0"}, nil, 1, 0},
		{"configured failure", []string{"sh", "-c", "exit 7"}, nil, 2, 7},
		{"invalid", []string{"sh", "-c", "exit 0"}, []string{"--priority", "6"}, 0, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls, priorities []int
			var startBody, finishBody struct {
				Metadata struct {
					Priorities map[string]int `json:"notification_priorities"`
				} `json:"metadata"`
			}
			worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, 1)
				if strings.HasSuffix(r.URL.Path, "/start") {
					json.NewDecoder(r.Body).Decode(&startBody)
				} else if strings.HasSuffix(r.URL.Path, "/finish") {
					json.NewDecoder(r.Body).Decode(&finishBody)
				}
				w.Write([]byte(`{"ok":true}`))
			}))
			defer worker.Close()
			ntfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Priority int `json:"priority"`
				}
				json.NewDecoder(r.Body).Decode(&body)
				priorities = append(priorities, body.Priority)
				w.WriteHeader(200)
			}))
			defer ntfy.Close()
			cfg := Config{URL: worker.URL, Token: "token", QueueDir: t.TempDir(), NtfyURL: ntfy.URL, NtfyTopic: "topic", MinSuccessSeconds: -1}
			if strings.HasPrefix(tc.name, "configured") {
				cfg.AlertPriorities = map[string]int{"success": 1, "failure": 2}
			}
			args := append(append(append([]string{"-n", "test"}, tc.flags...), "--"), tc.command...)
			var out, errOut bytes.Buffer
			code := runTask(cfg, args, strings.NewReader(""), &out, &errOut)
			if code != tc.code || (tc.priority != 0 && (len(priorities) != 1 || priorities[0] != tc.priority)) || (tc.priority == 0 && (len(calls) != 0 || len(priorities) != 0)) {
				t.Fatalf("code=%d sent=%v calls=%d errors=%s", code, priorities, len(calls), errOut.String())
			}
			if tc.name == "success" && (startBody.Metadata.Priorities["lost"] != 4 || startBody.Metadata.Priorities["recovered"] != 2) {
				t.Fatalf("start priority metadata=%+v", startBody.Metadata)
			}
			if tc.name == "success" && (finishBody.Metadata.Priorities["lost"] != 4 || finishBody.Metadata.Priorities["recovered"] != 2) {
				t.Fatalf("finish discarded priority metadata=%+v", finishBody.Metadata)
			}
		})
	}
}

func TestCodexPrioritySettingsPreserveExistingTemplates(t *testing.T) {
	t.Setenv("TB_CONFIG", filepath.Join(t.TempDir(), "codex.json"))
	cfg := Config{URL: "https://worker.example", Token: "secret", CodexStopTitle: "Existing title", CodexFinalOutput: true}
	if err := configureCodexHook(&cfg, []string{"--priority", "low", "--question-priority", "urgent", "--interrupt-priority", "min"}); err != nil {
		t.Fatal(err)
	}
	saved, err := loadConfig()
	if err != nil || saved.AlertPriorities["success"] != 2 || saved.AlertPriorities["question"] != 5 || saved.AlertPriorities["interrupt"] != 1 || saved.CodexStopTitle != "Existing title" || !saved.CodexFinalOutput {
		t.Fatalf("settings were not saved or templates changed: %v", err)
	}
	before, _ := os.ReadFile(configPath())
	if err := configureCodexHook(&cfg, []string{"--priority", "low", "--question-priority", "6"}); err == nil {
		t.Fatal("invalid priority accepted")
	}
	after, _ := os.ReadFile(configPath())
	if !bytes.Equal(before, after) {
		t.Fatal("invalid priority changed config")
	}
}

func TestMCPQuestionUsesConfiguredOrExplicitPriority(t *testing.T) {
	for _, tc := range []struct {
		value       string
		want, calls int
	}{
		{"", 5, 1}, {`,"priority":2`, 2, 1}, {`,"priority":6`, 0, 0},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("TB_CONFIG", filepath.Join(t.TempDir(), "codex.json"))
			var got, calls int
			worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/questions" {
					calls++
					var body struct {
						Priority int `json:"priority"`
					}
					json.NewDecoder(r.Body).Decode(&body)
					got = body.Priority
					w.Write([]byte(`{"ok":true,"id":"q1"}`))
					return
				}
				w.Write([]byte(`{"ok":true,"notifications":[]}`))
			}))
			defer worker.Close()
			input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"begin_question","arguments":{"question":"Continue?","type":"confirm"` + tc.value + `}}}` + "\n"
			var out bytes.Buffer
			serveMCP(Config{URL: worker.URL, Token: "token", AlertPriorities: map[string]int{"question": 5}}, strings.NewReader(input), &out)
			if got != tc.want || calls != tc.calls {
				t.Fatalf("priority=%d calls=%d response=%s", got, calls, out.String())
			}
			if tc.calls == 0 && !strings.Contains(out.String(), "priority") {
				t.Fatalf("missing error: %s", out.String())
			}
		})
	}
}

func TestExplicitAlertConfigDoesNotChangeOtherConfigs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TB_CONFIG", filepath.Join(dir, "config.json"))
	if err := saveConfig(Config{URL: "https://worker.example", Token: "secret"}); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "codex.json")
	original := []byte(`{"url":"https://other.example","token":"other"}`)
	if err := os.WriteFile(other, original, 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := execute([]string{"alerts", "--success", "low"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("code=%d errors=%s", code, errOut.String())
	}
	data, _ := os.ReadFile(other)
	if !bytes.Equal(original, data) {
		t.Fatal("TB_CONFIG command changed a different config")
	}
	if code := execute([]string{"init", "--url", "https://new.example", "--token", "new-token", "--ntfy-topic", "topic"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("code=%d errors=%s", code, errOut.String())
	}
	saved, err := loadConfig()
	if err != nil || saved.AlertPriorities["success"] != 2 || saved.URL != "https://new.example" {
		t.Fatal("reconfiguration discarded alert levels")
	}
}
