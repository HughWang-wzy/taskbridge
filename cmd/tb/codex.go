package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type hookInput struct {
	SessionID            string `json:"session_id"`
	TurnID               string `json:"turn_id"`
	Event                string `json:"hook_event_name"`
	ToolName             string `json:"tool_name"`
	ToolUseID            string `json:"tool_use_id"`
	CWD                  string `json:"cwd"`
	LastAssistantMessage string `json:"last_assistant_message"`
	ToolInput            struct {
		Questions []struct {
			Title string `json:"title"`
		} `json:"questions"`
	} `json:"tool_input"`
}

func configureCodexHook(c *Config, args []string) error {
	f := flag.NewFlagSet("hook codex", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	topic := f.String("topic", c.CodexTopic, "notification topic name")
	title := f.String("title", c.CodexStopTitle, "Stop notification title template")
	body := f.String("body", c.CodexStopBody, "Stop notification body template")
	final := f.String("final-output", "", "on or off")
	if e := f.Parse(args); e != nil {
		return fmt.Errorf("usage: tb hook codex [--topic TEXT] [--title TEMPLATE] [--body TEMPLATE] [--final-output on|off]: %w", e)
	}
	if f.NArg() > 0 {
		return fmt.Errorf("unexpected hook argument: %s", f.Arg(0))
	}
	if *final != "" && *final != "on" && *final != "off" {
		return fmt.Errorf("--final-output must be on or off")
	}
	if len(args) == 0 {
		return nil
	}
	c.CodexTopic, c.CodexStopTitle, c.CodexStopBody = *topic, *title, *body
	if *final != "" {
		c.CodexFinalOutput = *final == "on"
	}
	return saveConfig(*c)
}

func turnStartPath(input hookInput) string {
	key := sha256.Sum256([]byte(input.SessionID + "\x00" + input.TurnID))
	return filepath.Join(filepath.Dir(configPath()), "turn-start", fmt.Sprintf("%x", key))
}

func recordTurnStart(input hookInput) {
	path := turnStartPath(input)
	if os.MkdirAll(filepath.Dir(path), 0700) == nil {
		_ = os.WriteFile(path, []byte(fmt.Sprint(time.Now().UnixNano())), 0600)
	}
}

func turnDuration(input hookInput) string {
	path := turnStartPath(input)
	b, e := os.ReadFile(path)
	if e != nil {
		return "unknown"
	}
	_ = os.Remove(path)
	var started int64
	if _, e = fmt.Sscan(string(b), &started); e != nil || started <= 0 {
		return "unknown"
	}
	d := time.Since(time.Unix(0, started))
	if d < 0 {
		return "unknown"
	}
	return d.Truncate(time.Second).String()
}

func limitedFinalOutput(message string) string {
	message = strings.TrimSpace(message)
	runes := []rune(message)
	if len(runes) > 2000 {
		return string(runes[:2000]) + "… [truncated]"
	}
	return message
}

func stopNotification(c Config, input hookInput) ntfyMessage {
	topic := strings.TrimSpace(c.CodexTopic)
	if topic == "" {
		topic = filepath.Base(input.CWD)
	}
	if topic == "" || topic == "." {
		topic = "Codex"
	}
	title := c.CodexStopTitle
	if title == "" {
		title = "✅ {topic} finished"
	}
	body := c.CodexStopBody
	if body == "" {
		body = "{topic} task finished\nDuration: {duration}"
	}
	output := ""
	if c.CodexFinalOutput {
		output = limitedFinalOutput(input.LastAssistantMessage)
	}
	replacer := strings.NewReplacer("{topic}", topic, "{duration}", turnDuration(input), "{output}", output, "{session}", input.SessionID, "{turn}", input.TurnID)
	title, body = replacer.Replace(title), replacer.Replace(body)
	if c.CodexFinalOutput && output != "" && !strings.Contains(c.CodexStopBody, "{output}") {
		body += "\n\n" + output
	}
	return ntfyMessage{Title: title, Message: body, Priority: 3}
}

func questionMarkerPath(question string) string {
	key := sha256.Sum256([]byte(strings.TrimSpace(question)))
	return filepath.Join(filepath.Dir(configPath()), "question-mirror", fmt.Sprintf("%x", key))
}

func markQuestionMirrored(question string) {
	path := questionMarkerPath(question)
	if os.MkdirAll(filepath.Dir(path), 0700) == nil {
		_ = os.WriteFile(path, []byte(time.Now().UTC().Format(time.RFC3339)), 0600)
	}
}

func handleHook(c Config, in io.Reader, errOut io.Writer) int {
	var input hookInput
	if e := json.NewDecoder(in).Decode(&input); e != nil {
		fmt.Fprintln(errOut, e)
		return 0
	}
	if input.SessionID == "" || input.TurnID == "" {
		return 0
	}
	if input.Event == "UserPromptSubmit" {
		recordTurnStart(input)
		return 0
	}
	if input.Event == "PreToolUse" && (input.ToolName == "request_user_input_async" || input.ToolName == "request_user_input") {
		if len(input.ToolInput.Questions) == 0 || strings.TrimSpace(input.ToolInput.Questions[0].Title) == "" {
			return 0
		}
		question := strings.TrimSpace(input.ToolInput.Questions[0].Title)
		if info, e := os.Stat(questionMarkerPath(question)); e == nil && time.Since(info.ModTime()) < 2*time.Minute {
			return 0
		}
		id := fmt.Sprintf("codex:question:%x", sha256.Sum256([]byte(input.SessionID+"\x00"+input.TurnID+"\x00"+input.ToolUseID)))
		if e := sendSourceNotification(c, id, ntfyMessage{Title: "❓ Codex needs your answer", Message: question + "\nOpen Codex on your computer to answer.", Priority: 4}, errOut); e != nil {
			fmt.Fprintln(errOut, "TaskBridge question reminder:", e)
		}
		return 0
	}
	if input.Event == "Interrupt" {
		_ = os.Remove(turnStartPath(input))
		if e := spoolCodexInterrupt(input); e != nil {
			fmt.Fprintln(errOut, "TaskBridge interrupt spool:", e)
		}
		return 0
	}
	if input.Event != "Stop" && input.Event != "PostToolUse" {
		return 0
	}
	if input.Event == "PostToolUse" {
		key := sha256.Sum256([]byte(input.SessionID + "\x00" + input.TurnID))
		path := filepath.Join(filepath.Dir(configPath()), "hook-heartbeat", fmt.Sprintf("%x", key))
		if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			return 0
		}
		if info, e := os.Stat(path); e == nil && time.Since(info.ModTime()) < 120*time.Second {
			return 0
		}
		_ = os.WriteFile(path, []byte(time.Now().UTC().Format(time.RFC3339)), 0600)
	}
	var notice ntfyMessage
	if input.Event == "Stop" {
		notice = stopNotification(c, input)
	}
	event := map[string]any{"session_id": input.SessionID, "turn_id": input.TurnID, "event": input.Event}
	data, e := api(c, "POST", "/v1/codex/events", event)
	if e != nil {
		fmt.Fprintln(errOut, "TaskBridge hook:", e)
		_ = queue(c, "POST", "/v1/codex/events", event)
		if input.Event != "PostToolUse" {
			id := fmt.Sprintf("codex:%x", sha256.Sum256([]byte(input.SessionID+"\x00"+input.TurnID+"\x00"+input.Event)))
			_ = queue(c, "POST", "/v1/notifications", map[string]any{"id": id, "payload": notice})
		}
		return 0
	}
	var result struct {
		Duplicate bool `json:"duplicate"`
	}
	_ = json.Unmarshal(data, &result)
	if input.Event != "PostToolUse" && !result.Duplicate {
		id := fmt.Sprintf("codex:%x", sha256.Sum256([]byte(input.SessionID+"\x00"+input.TurnID+"\x00"+input.Event)))
		if noticeErr := sendSourceNotification(c, id, notice, errOut); noticeErr != nil {
			fmt.Fprintln(errOut, "TaskBridge hook:", noticeErr)
		}
	}
	return 0
}
func installCodexHooks(out io.Writer) error {
	home, e := os.UserHomeDir()
	if e != nil {
		return e
	}
	path := filepath.Join(home, ".codex", "hooks.json")
	doc := map[string]any{}
	if b, e := os.ReadFile(path); e == nil {
		if e = json.Unmarshal(b, &doc); e != nil {
			return fmt.Errorf("parse existing hooks: %w", e)
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	hooks, ok := doc["hooks"].(map[string]any)
	if !ok {
		hooks = map[string]any{}
		doc["hooks"] = hooks
	}
	executable, e := os.Executable()
	if e != nil {
		return e
	}
	// Codex executes hook commands in a shell. Quote the absolute executable path.
	command := "'" + strings.ReplaceAll(executable, "'", "'\\''") + "' hook codex event"
	if runtime.GOOS == "windows" {
		command = "\"" + executable + "\" hook codex event"
	}
	for _, event := range []string{"Stop", "Interrupt", "PostToolUse", "PreToolUse", "UserPromptSubmit"} {
		existing, _ := hooks[event].([]any)
		filtered := make([]any, 0, len(existing)+1)
		for _, entry := range existing {
			b, _ := json.Marshal(entry)
			if !strings.Contains(string(b), " hook codex event") {
				filtered = append(filtered, entry)
			}
		}
		timeout := 30
		if event == "Interrupt" {
			timeout = 3
		}
		entry := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command, "async": true, "timeout": timeout}}}
		if event == "PreToolUse" {
			entry["matcher"] = "^request_user_input(_async)?$"
		}
		filtered = append(filtered, entry)
		hooks[event] = filtered
	}
	b, e := json.MarshalIndent(doc, "", "  ")
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	if e = os.WriteFile(path, b, 0600); e != nil {
		return e
	}
	fmt.Fprintln(out, "Codex hooks installed:", path)
	fmt.Fprintln(out, "Codex will ask you to review and trust these hooks on next start.")
	return nil
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func rpcReply(out io.Writer, id json.RawMessage, result any, errMessage string) {
	if len(id) == 0 {
		return
	}
	response := map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id)}
	if errMessage != "" {
		response["error"] = map[string]any{"code": -32603, "message": errMessage}
	} else {
		response["result"] = result
	}
	b, _ := json.Marshal(response)
	fmt.Fprintln(out, string(b))
}
func serveMCP(c Config, in io.Reader, out io.Writer) {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	for scanner.Scan() {
		var msg rpcMessage
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			continue
		}
		switch msg.Method {
		case "initialize":
			rpcReply(out, msg.ID, map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "taskbridge", "version": "1.0.0"}}, "")
		case "tools/list":
			createSchema := map[string]any{"type": "object", "properties": map[string]any{"question": map[string]any{"type": "string"}, "type": map[string]any{"type": "string", "enum": []string{"confirm", "choice", "text"}}, "options": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "timeout_seconds": map[string]any{"type": "integer", "minimum": 60, "maximum": 86400}, "session_id": map[string]any{"type": "string"}, "turn_id": map[string]any{"type": "string"}}, "required": []string{"question", "type"}}
			idSchema := map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}}, "required": []string{"id"}}
			rpcReply(out, msg.ID, map[string]any{"tools": []any{
				map[string]any{"name": "ask_user", "description": "Ask the user on their phone and wait for an answer. Use for confirm, choice, or free text decisions.", "inputSchema": createSchema},
				map[string]any{"name": "begin_question", "description": "Create one question, notify the phone, and return its id immediately. Use before showing the same question in Codex's native desktop prompt.", "inputSchema": createSchema},
				map[string]any{"name": "question_status", "description": "Read the current winning answer for a TaskBridge question.", "inputSchema": idSchema},
				map[string]any{"name": "answer_question", "description": "Submit the desktop answer. The first answer from phone or desktop wins; the response contains the winning answer.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}, "answer": map[string]any{"type": "string"}}, "required": []string{"id", "answer"}}},
			}}, "")
		case "tools/call":
			var call struct {
				Name      string `json:"name"`
				Arguments struct {
					Question  string   `json:"question"`
					Type      string   `json:"type"`
					Options   []string `json:"options"`
					Timeout   int      `json:"timeout_seconds"`
					SessionID string   `json:"session_id"`
					TurnID    string   `json:"turn_id"`
					ID        string   `json:"id"`
					Answer    string   `json:"answer"`
				} `json:"arguments"`
			}
			if e := json.Unmarshal(msg.Params, &call); e != nil {
				rpcReply(out, msg.ID, nil, "invalid tool call")
				continue
			}
			if call.Name == "question_status" || call.Name == "answer_question" {
				if call.Arguments.ID == "" {
					rpcReply(out, msg.ID, nil, "missing question id")
					continue
				}
				path := "/v1/questions/" + url.PathEscape(call.Arguments.ID)
				method := "GET"
				var body any
				if call.Name == "answer_question" {
					method, path, body = "POST", path+"/answer-desktop", map[string]any{"answer": call.Arguments.Answer}
				}
				data, e := api(c, method, path, body)
				if e != nil {
					rpcReply(out, msg.ID, map[string]any{"isError": true, "content": []any{map[string]string{"type": "text", "text": e.Error()}}}, "")
				} else {
					rpcReply(out, msg.ID, map[string]any{"content": []any{map[string]string{"type": "text", "text": string(data)}}}, "")
				}
				continue
			}
			if call.Name != "ask_user" && call.Name != "begin_question" {
				rpcReply(out, msg.ID, nil, "invalid tool call")
				continue
			}
			timeout := call.Arguments.Timeout
			if timeout <= 0 {
				timeout = 3600
			}
			if timeout > 86400 {
				timeout = 86400
			}
			data, e := api(c, "POST", "/v1/questions", map[string]any{"question_type": call.Arguments.Type, "question": call.Arguments.Question, "options": call.Arguments.Options, "timeout_seconds": timeout, "session_id": call.Arguments.SessionID, "turn_id": call.Arguments.TurnID})
			if e != nil {
				rpcReply(out, msg.ID, map[string]any{"isError": true, "content": []any{map[string]string{"type": "text", "text": e.Error()}}}, "")
				continue
			}
			var created struct {
				ID string `json:"id"`
			}
			if e = json.Unmarshal(data, &created); e != nil || created.ID == "" {
				rpcReply(out, msg.ID, nil, "invalid question response")
				continue
			}
			if _, relayErr := relayOnceWithID(c, "question:"+created.ID+":created"); relayErr != nil {
				fmt.Fprintln(os.Stderr, "TaskBridge MCP notification:", relayErr)
			}
			markQuestionMirrored(call.Arguments.Question)
			if call.Name == "begin_question" {
				rpcReply(out, msg.ID, map[string]any{"content": []any{map[string]string{"type": "text", "text": string(data)}}}, "")
				continue
			}
			deadline := time.Now().Add(time.Duration(timeout) * time.Second)
			for {
				if time.Now().After(deadline) {
					rpcReply(out, msg.ID, map[string]any{"isError": true, "content": []any{map[string]string{"type": "text", "text": "Question timed out"}}}, "")
					break
				}
				time.Sleep(2 * time.Second)
				result, e := api(c, "GET", "/v1/questions/"+created.ID, nil)
				if e != nil {
					continue
				}
				var poll struct {
					Question struct {
						Status string `json:"status"`
						Answer string `json:"answer"`
					} `json:"question"`
				}
				if json.Unmarshal(result, &poll) != nil {
					continue
				}
				if poll.Question.Status == "answered" {
					rpcReply(out, msg.ID, map[string]any{"content": []any{map[string]string{"type": "text", "text": poll.Question.Answer}}}, "")
					break
				}
				if poll.Question.Status == "expired" {
					rpcReply(out, msg.ID, map[string]any{"isError": true, "content": []any{map[string]string{"type": "text", "text": "Question expired"}}}, "")
					break
				}
			}
		default:
			if len(msg.ID) > 0 {
				rpcReply(out, msg.ID, nil, "method not found")
			}
		}
	}
}
