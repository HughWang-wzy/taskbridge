package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRelayDoesNotPublishExpiredClaim(t *testing.T) {
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/claim") {
			fmt.Fprintf(w, `{"notifications":[{"id":"old","claim_token":"c","expires_at":%d,"payload":{"title":"Old","message":"Old"}}]}`, time.Now().Add(-time.Second).UnixMilli())
			return
		}
		t.Error("expired notice must not be ACKed or retried")
	}))
	defer worker.Close()
	ntfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("expired notice published") }))
	defer ntfy.Close()
	n, err := relayOnce(Config{URL: worker.URL, NtfyURL: ntfy.URL, NtfyTopic: "topic"})
	if err != nil || n != 0 {
		t.Fatalf("%d %v", n, err)
	}
}

func TestOfflineNotificationExpiryRetainsOriginalTimeAndArchivesOldFiles(t *testing.T) {
	var received map[string]any
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer worker.Close()
	c := Config{URL: worker.URL, QueueDir: t.TempDir()}
	if err := queue(c, "POST", "/v1/notifications", map[string]any{"id": "new", "payload": map[string]any{"message": "new"}}); err != nil {
		t.Fatal(err)
	}
	items, _ := readQueue(c.QueueDir)
	var saved map[string]any
	json.Unmarshal(items[0].Body, &saved)
	if _, ok := saved["created_at"]; !ok {
		t.Fatal("notification missing original creation time")
	}
	if err := flushQueue(c); err != nil {
		t.Fatal(err)
	}
	if received["created_at"] != saved["created_at"] {
		t.Fatal("replay reset delivery window")
	}
	received = nil
	legacy := filepath.Join(c.QueueDir, "legacy.json")
	os.WriteFile(legacy, []byte(`{"method":"POST","path":"/v1/notifications","body":{"id":"old","payload":{"message":"old"}}}`), 0600)
	old := time.Now().Add(-6 * time.Minute)
	os.Chtimes(legacy, old, old)
	if err := flushQueue(c); err != nil {
		t.Fatal(err)
	}
	if received != nil {
		t.Fatal("expired offline notification replayed")
	}
	if _, err := os.Stat(filepath.Join(c.QueueDir, "expired", "legacy.json")); err != nil {
		t.Fatal("expired record was not archived", err)
	}
}

func TestExpiredInterruptSpoolIsArchivedWithoutSending(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "old.json")
	os.WriteFile(p, []byte(`{"hook_event_name":"Interrupt","session_id":"s","turn_id":"t"}`), 0600)
	old := time.Now().Add(-6 * time.Minute)
	os.Chtimes(p, old, old)
	if err := flushCodexInterrupts(Config{URL: "http://127.0.0.1:1"}, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "expired", "old.json")); err != nil {
		t.Fatal(err)
	}
}
