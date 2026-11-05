package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
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
	if e := sendSourceNotification(cfg, "task:abc:finished", ntfyMessage{Title: "Failed", Message: "exit 1", Priority: 2}, &errOut); e != nil {
		t.Fatal(e)
	}
	if queued.ID != "task:abc:finished" || queued.Payload.Message != "exit 1" || queued.Payload.Priority != 2 {
		t.Fatalf("queued=%+v", queued)
	}
}

func TestRelayClaimsPublishesAndAcknowledges(t *testing.T) {
	var acked string
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/notifications/claim":
			w.Write([]byte(`{"ok":true,"notifications":[{"id":"task:lost:1","claim_token":"claim-1","payload":{"title":"Task LOST","message":"Training","priority":5}}]}`))
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
	cfg := Config{URL: worker.URL, Token: "token", NtfyURL: ntfy.URL, NtfyTopic: "phone-topic", AlertPriorities: map[string]int{"lost": 1}}
	count, e := relayOnce(cfg)
	if e != nil || count != 1 || acked != "claim-1" || published["topic"] != "phone-topic" || published["priority"] != float64(5) {
		t.Fatalf("count=%d err=%v ack=%s published=%v", count, e, acked, published)
	}
}

func TestPublishFitsNtfyUTF8ByteLimit(t *testing.T) {
	for _, original := range []string{strings.Repeat("x", 4095), strings.Repeat("x", 4096), strings.Repeat("中", 2000), strings.Repeat("a🙂", 1200)} {
		t.Run(string([]rune(original)[:1]), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var got ntfyMessage
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				if len(got.Message) >= 4096 || !utf8.ValidString(got.Message) {
					http.Error(w, "message exceeds ntfy byte limit", 500)
					return
				}
				if len(original) < 4096 && got.Message != original {
					t.Error("short message changed")
				}
				if len(original) >= 4096 && !strings.HasSuffix(got.Message, "… [truncated for ntfy]") {
					t.Error("truncation must be visible")
				}
			}))
			defer server.Close()
			message := ntfyMessage{Title: "Title", Message: original}
			if err := publishNtfy(Config{NtfyURL: server.URL, NtfyTopic: "topic"}, message); err != nil {
				t.Fatal(err)
			}
			if message.Message != original {
				t.Fatal("stored payload was changed")
			}
		})
	}
}
