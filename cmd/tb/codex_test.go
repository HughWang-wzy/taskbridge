package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInterruptSpoolsQuicklyAndRelaySubmitsOnce(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TB_CONFIG", filepath.Join(dir, "codex.json"))
	var eventBody, noticeBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		switch r.URL.Path {
		case "/v1/codex/events":
			_ = json.NewDecoder(r.Body).Decode(&eventBody)
		case "/v1/notifications":
			_ = json.NewDecoder(r.Body).Decode(&noticeBody)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	config := Config{URL: server.URL, Token: "token"}
	var errOut bytes.Buffer
	start := time.Now()
	input := `{"session_id":"s1","turn_id":"t1","hook_event_name":"Interrupt"}`
	if code := handleHook(config, strings.NewReader(input), &errOut); code != 0 || time.Since(start) > 200*time.Millisecond {
		t.Fatalf("hook blocked: code=%d elapsed=%s errors=%s", code, time.Since(start), errOut.String())
	}
	if eventBody != nil || noticeBody != nil {
		t.Fatal("Interrupt hook performed network I/O")
	}
	if e := flushCodexInterrupts(config, filepath.Join(dir, "codex-interrupts")); e != nil {
		t.Fatal(e)
	}
	if eventBody["event"] != "Interrupt" || noticeBody["id"] == "" {
		t.Fatalf("event=%v notice=%v", eventBody, noticeBody)
	}
	entries, e := os.ReadDir(filepath.Join(dir, "codex-interrupts"))
	if e != nil || len(entries) != 0 {
		t.Fatalf("spool not cleared: entries=%v err=%v", entries, e)
	}
}

func TestHookSendsStopEvent(t *testing.T) {
	var body map[string]any
	published := 0
	ntfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { published++; w.WriteHeader(200) }))
	defer ntfy.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{"ok":true,"duplicate":false}`))
	}))
	defer server.Close()
	var errOut bytes.Buffer
	code := handleHook(Config{URL: server.URL, Token: "token", QueueDir: t.TempDir(), NtfyURL: ntfy.URL, NtfyTopic: "phone-topic"}, strings.NewReader(`{"session_id":"s1","turn_id":"t1","hook_event_name":"Stop"}`), &errOut)
	if code != 0 || body["event"] != "Stop" || body["session_id"] != "s1" || published != 1 {
		t.Fatalf("code=%d body=%v published=%d errors=%s", code, body, published, errOut.String())
	}
}

func TestMCPListsAskUser(t *testing.T) {
	input := "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/list\"}\n"
	var out bytes.Buffer
	serveMCP(Config{}, strings.NewReader(input), &out)
	if !strings.Contains(out.String(), `"name":"ask_user"`) {
		t.Fatalf("output=%s", out.String())
	}
	for _, name := range []string{"begin_question", "question_status", "answer_question"} {
		if !strings.Contains(out.String(), `"name":"`+name+`"`) {
			t.Fatalf("missing %s in %s", name, out.String())
		}
	}
}

func TestMCPBeginAndAnswerQuestion(t *testing.T) {
	t.Setenv("TB_CONFIG", t.TempDir()+"/codex.json")
	published := 0
	ntfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { published++; w.WriteHeader(200) }))
	defer ntfy.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/questions":
			w.Write([]byte(`{"ok":true,"id":"q1"}`))
		case "/v1/notifications/claim":
			w.Write([]byte(`{"ok":true,"notifications":[{"id":"question:q1:created","claim_token":"lease-1","payload":{"title":"Question","message":"Proceed?"}}]}`))
		case "/v1/notifications/question:q1:created/ack":
			w.Write([]byte(`{"ok":true}`))
		case "/v1/questions/q1/answer-desktop":
			var body struct {
				Answer string `json:"answer"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if body.Answer != "Continue" {
				t.Errorf("answer=%q", body.Answer)
			}
			w.Write([]byte(`{"ok":true,"answer":"Continue","duplicate":false}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"begin_question","arguments":{"type":"choice","question":"Proceed?","options":["Continue","Cancel"]}}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"answer_question","arguments":{"id":"q1","answer":"Continue"}}}` + "\n"
	var out bytes.Buffer
	serveMCP(Config{URL: server.URL, Token: "token", NtfyURL: ntfy.URL, NtfyTopic: "topic"}, strings.NewReader(input), &out)
	if published != 1 || !strings.Contains(out.String(), `\"id\":\"q1\"`) || !strings.Contains(out.String(), `\"answer\":\"Continue\"`) {
		t.Fatalf("published=%d output=%s", published, out.String())
	}
}

func TestNativeQuestionHookSendsPhoneReminder(t *testing.T) {
	t.Setenv("TB_CONFIG", t.TempDir()+"/codex.json")
	published := 0
	ntfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { published++; w.WriteHeader(200) }))
	defer ntfy.Close()
	cfg := Config{URL: "http://127.0.0.1:1", Token: "token", NtfyURL: ntfy.URL, NtfyTopic: "topic", QueueDir: t.TempDir()}
	input := `{"session_id":"s","turn_id":"t","hook_event_name":"PreToolUse","tool_name":"request_user_input_async","tool_use_id":"u","tool_input":{"questions":[{"title":"Proceed?"}]}}`
	var errOut bytes.Buffer
	handleHook(cfg, strings.NewReader(input), &errOut)
	markQuestionMirrored("Proceed?")
	handleHook(cfg, strings.NewReader(input), &errOut)
	if published != 1 {
		t.Fatalf("published=%d errors=%s", published, errOut.String())
	}
}

func TestMCPAskUserReturnsAnswer(t *testing.T) {
	var published, acked bool
	ntfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { published = true; w.WriteHeader(200) }))
	defer ntfy.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/questions" {
			w.Write([]byte(`{"ok":true,"id":"q1"}`))
			return
		}
		if r.URL.Path == "/v1/notifications/claim" {
			w.Write([]byte(`{"ok":true,"notifications":[{"id":"question:q1:created","claim_token":"lease-1","payload":{"title":"Question","message":"Continue?"}}]}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/ack") {
			acked = true
			w.Write([]byte(`{"ok":true}`))
			return
		}
		w.Write([]byte(`{"ok":true,"question":{"status":"answered","answer":"yes"}}`))
	}))
	defer server.Close()
	input := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ask_user","arguments":{"type":"confirm","question":"Continue?"}}}` + "\n"
	var out bytes.Buffer
	serveMCP(Config{URL: server.URL, Token: "token", NtfyURL: ntfy.URL, NtfyTopic: "topic"}, strings.NewReader(input), &out)
	if !strings.Contains(out.String(), `"text":"yes"`) || !published || !acked {
		t.Fatalf("output=%s published=%t acked=%t", out.String(), published, acked)
	}
}

func TestHookQueuesDeterministicNoticeWhenWorkerIsUnavailable(t *testing.T) {
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "down", 503) }))
	defer worker.Close()
	sends := 0
	ntfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { sends++; w.WriteHeader(200) }))
	defer ntfy.Close()
	dir := t.TempDir()
	var errOut bytes.Buffer
	cfg := Config{URL: worker.URL, Token: "token", QueueDir: dir, NtfyURL: ntfy.URL, NtfyTopic: "topic"}
	handleHook(cfg, strings.NewReader(`{"session_id":"s2","turn_id":"t2","hook_event_name":"Stop"}`), &errOut)
	entries, e := readQueue(dir)
	if e != nil || sends != 0 || len(entries) != 2 {
		t.Fatalf("sends=%d entries=%v err=%v", sends, entries, e)
	}
}
