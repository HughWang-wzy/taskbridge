package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const notificationWindow = 5 * time.Minute

// Keep the source time across offline replay; receiving it later must not
// create a new delivery window. Existing queue files use their original mtime.
func prepareNotificationBody(body any, created time.Time) (map[string]any, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	var object map[string]any
	if err = json.Unmarshal(data, &object); err != nil {
		return nil, err
	}
	if object == nil {
		return nil, fmt.Errorf("notification body must be an object")
	}
	if _, exists := object["created_at"]; !exists {
		object["created_at"] = float64(created.UnixMilli())
	}
	return object, nil
}

func archiveExpiredNotification(path string) error {
	dir := filepath.Join(filepath.Dir(path), "expired")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return os.Rename(path, filepath.Join(dir, filepath.Base(path)))
}
