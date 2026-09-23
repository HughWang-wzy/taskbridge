package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type ntfyMessage struct {
	Title    string           `json:"title"`
	Message  string           `json:"message"`
	Priority int              `json:"priority,omitempty"`
	Tags     []string         `json:"tags,omitempty"`
	Actions  []map[string]any `json:"actions,omitempty"`
}

func publishNtfy(c Config, message ntfyMessage) error {
	if strings.TrimSpace(c.NtfyTopic) == "" {
		return errors.New("ntfy topic is not configured; run tb init --ntfy-topic TOPIC")
	}
	endpoint := c.NtfyURL
	if endpoint == "" {
		endpoint = "https://ntfy.sh/"
	}
	parsed, e := url.Parse(endpoint)
	if e != nil || parsed.Host == "" {
		return errors.New("invalid ntfy URL")
	}
	body, e := json.Marshal(struct {
		Topic string `json:"topic"`
		ntfyMessage
	}{c.NtfyTopic, message})
	if e != nil {
		return e
	}
	req, e := http.NewRequest("POST", endpoint, bytes.NewReader(body))
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 TaskBridge/1.0")
	if c.NtfyToken != "" {
		token := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(c.NtfyToken), "Bearer "))
		req.Header.Set("Authorization", "Bearer "+token)
	}
	// Direct publication must use this machine's egress address, not a shared proxy.
	httpClient := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil}}
	res, e := httpClient.Do(req)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 500))
		return fmt.Errorf("ntfy HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func sendSourceNotification(c Config, id string, message ntfyMessage, errOut io.Writer) error {
	if e := publishNtfy(c, message); e == nil {
		return nil
	} else {
		fmt.Fprintf(errOut, "TaskBridge: direct ntfy failed, queueing notification: %v\n", e)
	}
	body := map[string]any{"id": id, "payload": message}
	if _, e := api(c, "POST", "/v1/notifications", body); e == nil {
		return nil
	}
	if e := queue(c, "POST", "/v1/notifications", body); e != nil {
		return fmt.Errorf("notification queue failed: %w", e)
	}
	return nil
}

type relayItem struct {
	ID         string      `json:"id"`
	ClaimToken string      `json:"claim_token"`
	Payload    ntfyMessage `json:"payload"`
}

func relayOnce(c Config) (int, error) {
	return relayOnceWithID(c, "")
}

func relayOnceWithID(c Config, id string) (int, error) {
	claim := map[string]any{"limit": 10}
	if id != "" {
		claim["id"] = id
	}
	data, e := api(c, "POST", "/v1/notifications/claim", claim)
	if e != nil {
		return 0, e
	}
	var response struct {
		Notifications []relayItem `json:"notifications"`
	}
	if e = json.Unmarshal(data, &response); e != nil {
		return 0, e
	}
	delivered := 0
	var firstErr error
	for _, item := range response.Notifications {
		route := "/v1/notifications/" + url.PathEscape(item.ID)
		if e = publishNtfy(c, item.Payload); e != nil {
			_, failErr := api(c, "POST", route+"/fail", map[string]any{"claim_token": item.ClaimToken})
			if failErr != nil {
				e = fmt.Errorf("publish: %w; release claim: %v", e, failErr)
			}
			if firstErr == nil {
				firstErr = e
			}
			continue
		}
		var ackErr error
		for attempt := 0; attempt < 3; attempt++ {
			_, ackErr = api(c, "POST", route+"/ack", map[string]any{"claim_token": item.ClaimToken})
			if ackErr == nil {
				break
			}
			time.Sleep(time.Second)
		}
		if ackErr != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("published %s but ACK failed: %w", item.ID, ackErr)
			}
		} else {
			delivered++
		}
	}
	return delivered, firstErr
}
