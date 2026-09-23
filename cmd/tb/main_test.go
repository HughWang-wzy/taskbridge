package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

func TestRunPreservesOutputAndExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell syntax differs")
	}
	for _, tc := range []struct {
		name   string
		exit   int
		output string
	}{
		{"success", 0, "hello"}, {"failure", 7, "bad"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			var published []map[string]any
			ntfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				published = append(published, body)
				w.Write([]byte(`{"ok":true}`))
			}))
			defer ntfy.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, r.URL.Path)
				if strings.HasSuffix(r.URL.Path, "/start") {
					json.NewEncoder(w).Encode(map[string]any{"ok": true})
				} else {
					json.NewEncoder(w).Encode(map[string]any{"ok": true})
				}
			}))
			defer server.Close()
			var out, errOut bytes.Buffer
			cfg := Config{URL: server.URL, Token: "token", QueueDir: t.TempDir(), NtfyURL: ntfy.URL, NtfyTopic: "phone-topic", MinSuccessSeconds: -1}
			code := runTask(cfg, []string{"-n", tc.name, "--", "sh", "-c", "echo " + tc.output + "; exit " + string(rune('0'+tc.exit))}, strings.NewReader(""), &out, &errOut)
			if code != tc.exit {
				t.Fatalf("exit=%d want=%d", code, tc.exit)
			}
			if !strings.Contains(out.String(), tc.output) {
				t.Fatalf("output=%q", out.String())
			}
			if len(calls) != 2 || calls[0] != "/v1/tasks/start" || !strings.HasSuffix(calls[1], "/finish") {
				t.Fatalf("calls=%v", calls)
			}
			if len(published) != 1 || published[0]["topic"] != "phone-topic" || published[0]["message"]=="" {
				t.Fatalf("published=%v", published)
			}
		})
	}
}

func TestRunQueuesFinishWhenServerFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell syntax differs")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/finish") {
			http.Error(w, "down", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	ntfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer ntfy.Close()
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	code := runTask(Config{URL: server.URL, Token: "token", QueueDir: dir, NtfyURL: ntfy.URL, NtfyTopic: "topic"}, []string{"-n", "test", "--", "sh", "-c", "exit 1"}, strings.NewReader(""), &out, &errOut)
	if code != 1 {
		t.Fatalf("exit=%d", code)
	}
	entries, err := readQueue(dir)
	if err != nil || len(entries) != 1 || !strings.HasSuffix(entries[0].Path, "/finish") {
		t.Fatalf("queue=%v err=%v", entries, err)
	}
}

func TestDoctorUsesAuthenticatedEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/doctor" || r.Header.Get("Authorization") != "Bearer token" {
			http.Error(w, "bad request", 404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true,"pending_notifications":0}`))
	}))
	defer server.Close()
	path := t.TempDir() + "/config.json"
	t.Setenv("TB_CONFIG", path)
	if e := saveConfig(Config{URL: server.URL, Token: "token"}); e != nil {
		t.Fatal(e)
	}
	var out, errOut bytes.Buffer
	code := execute([]string{"doctor"}, strings.NewReader(""), &out, &errOut)
	if code != 0 || !strings.Contains(out.String(), "pending_notifications") {
		t.Fatalf("code=%d output=%s errors=%s", code, out.String(), errOut.String())
	}
}
