package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func codexInterruptDir(configFile string) string {
	return filepath.Join(filepath.Dir(configFile), "codex-interrupts")
}

func spoolCodexInterrupt(input hookInput) error {
	dir := codexInterruptDir(configPath())
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	id := sha256.Sum256([]byte(input.SessionID + "\x00" + input.TurnID + "\x00Interrupt"))
	name := fmt.Sprintf("%x.json", id)
	data, e := json.Marshal(input)
	if e != nil {
		return e
	}
	tmp := filepath.Join(dir, "."+name+"-"+newID())
	if e = os.WriteFile(tmp, data, 0600); e != nil {
		return e
	}
	if e = os.Rename(tmp, filepath.Join(dir, name)); e != nil {
		_ = os.Remove(tmp)
		return e
	}
	return nil
}

func flushCodexInterrupts(c Config, dir string) error {
	entries, e := os.ReadDir(dir)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		var input hookInput
		if e = json.Unmarshal(data, &input); e != nil {
			return e
		}
		if input.Event != "Interrupt" || input.SessionID == "" || input.TurnID == "" {
			return fmt.Errorf("invalid Codex interrupt spool: %s", entry.Name())
		}
		event := map[string]any{"session_id": input.SessionID, "turn_id": input.TurnID, "event": "Interrupt"}
		if _, e = api(c, "POST", "/v1/codex/events", event); e != nil {
			return e
		}
		id := fmt.Sprintf("codex:%x", sha256.Sum256([]byte(input.SessionID+"\x00"+input.TurnID+"\x00Interrupt")))
		notice := ntfyMessage{Title: "⚠️ Codex interrupted", Message: "Session: " + input.SessionID + "\nTurn: " + input.TurnID, Priority: 4}
		if _, e = api(c, "POST", "/v1/notifications", map[string]any{"id": id, "payload": notice}); e != nil {
			return e
		}
		if e = os.Remove(path); e != nil {
			return e
		}
	}
	return nil
}

func flushUserCodexInterrupts() error {
	dir, e := os.UserConfigDir()
	if e != nil {
		return e
	}
	path := filepath.Join(dir, "taskbridge", "codex.json")
	data, e := os.ReadFile(path)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	var c Config
	if e = json.Unmarshal(data, &c); e != nil {
		return e
	}
	return flushCodexInterrupts(c, codexInterruptDir(path))
}
