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

func TestShortSuccessfulRunDoesNotNotify(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell syntax differs")
	}
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"ok":true}`)) }))
	defer worker.Close()
	sends := 0
	ntfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { sends++; w.WriteHeader(200) }))
	defer ntfy.Close()
	var out, errOut bytes.Buffer
	code := runTask(Config{URL: worker.URL, Token: "token", NtfyURL: ntfy.URL, NtfyTopic: "topic", QueueDir: t.TempDir()}, []string{"-n", "short", "--", "sh", "-c", "exit 0"}, strings.NewReader(""), &out, &errOut)
	if code != 0 || sends != 0 {
		t.Fatalf("code=%d sends=%d errors=%s", code, sends, errOut.String())
	}
}

func TestDirectPublishFailureFallsBackToWorkerQueue(t *testing.T) {
	var queued struct {
		ID      string      `json:"id"`
		Payload ntfyMessage `json:"payload"`
	}
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/notifications" {
			http.Error(w, "wrong path", 404)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&queued)
		w.WriteHeader(201)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer worker.Close()
	ntfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "quota", 429) }))
	defer ntfy.Close()
	var errOut bytes.Buffer
	cfg := Config{URL: worker.URL, Token: "token", NtfyURL: ntfy.URL, NtfyTopic: "topic", QueueDir: t.TempDir()}
	if e := sendSourceNotification(cfg, "task:abc:finished", ntfyMessage{Title: "Failed", Message: "exit 1"}, &errOut); e != nil {
		t.Fatal(e)
	}
	if queued.ID != "task:abc:finished" || queued.Payload.Message != "exit 1" {
		t.Fatalf("queued=%+v", queued)
	}
}

func TestRelayClaimsPublishesAndAcknowledges(t *testing.T) {
	var acked string
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/notifications/claim":
			w.Write([]byte(`{"ok":true,"notifications":[{"id":"task:lost:1","claim_token":"claim-1","payload":{"title":"Task LOST","message":"Training"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/ack"):
			var body struct {
				ClaimToken string `json:"claim_token"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			acked = body.ClaimToken
			w.Write([]byte(`{"ok":true}`))
		default:
			http.Error(w, "wrong path", 404)
		}
	}))
	defer worker.Close()
	var published map[string]any
	ntfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&published)
		w.WriteHeader(200)
	}))
	defer ntfy.Close()
	cfg := Config{URL: worker.URL, Token: "token", NtfyURL: ntfy.URL, NtfyTopic: "phone-topic"}
	count, e := relayOnce(cfg)
	if e != nil || count != 1 || acked != "claim-1" || published["topic"] != "phone-topic" {
		t.Fatalf("count=%d err=%v ack=%s published=%v", count, e, acked, published)
	}
}
