package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

type Config struct {
	URL               string `json:"url"`
	Token             string `json:"token"`
	NtfyTopic         string `json:"ntfy_topic,omitempty"`
	NtfyToken         string `json:"ntfy_token,omitempty"`
	NtfyURL           string `json:"ntfy_url,omitempty"`
	MinSuccessSeconds int    `json:"min_success_seconds,omitempty"`
	QueueDir          string `json:"-"`
	HeartbeatSeconds  int    `json:"heartbeat_seconds,omitempty"`
	CodexTopic        string `json:"codex_topic,omitempty"`
	CodexStopTitle    string `json:"codex_stop_title,omitempty"`
	CodexStopBody     string `json:"codex_stop_body,omitempty"`
	CodexFinalOutput  bool   `json:"codex_final_output,omitempty"`
}
type queuedRequest struct {
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Body   json.RawMessage `json:"body"`
}
type queueEntry struct {
	Filename string
	queuedRequest
}

func configPath() string {
	if path := os.Getenv("TB_CONFIG"); path != "" {
		return path
	}
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "taskbridge", "config.json")
}
func loadConfig() (Config, error) {
	var c Config
	b, e := os.ReadFile(configPath())
	if e != nil {
		return c, e
	}
	e = json.Unmarshal(b, &c)
	if c.QueueDir == "" {
		c.QueueDir = filepath.Join(filepath.Dir(configPath()), "queue")
	}
	return c, e
}
func saveConfig(c Config) error {
	p := configPath()
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		return e
	}
	b, e := json.MarshalIndent(c, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(p, b, 0600)
}
func newID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func client() *http.Client { return &http.Client{Timeout: 10 * time.Second} }
func api(c Config, method, path string, body any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			return nil, e
		}
		reader = bytes.NewReader(b)
	}
	req, e := http.NewRequest(method, strings.TrimRight(c.URL, "/")+path, reader)
	if e != nil {
		return nil, e
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("User-Agent", "Mozilla/5.0 TaskBridge/1.0")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, e := client().Do(req)
	if e != nil {
		return nil, e
	}
	defer res.Body.Close()
	data, e := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if e != nil {
		return nil, e
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, nil
}
func queue(c Config, method, path string, body any) error {
	if e := os.MkdirAll(c.QueueDir, 0700); e != nil {
		return e
	}
	b, e := json.Marshal(body)
	if e != nil {
		return e
	}
	item, e := json.Marshal(queuedRequest{method, path, b})
	if e != nil {
		return e
	}
	name := fmt.Sprintf("%020d-%s.json", time.Now().UnixNano(), newID())
	tmp := filepath.Join(c.QueueDir, "."+name)
	if e = os.WriteFile(tmp, item, 0600); e != nil {
		return e
	}
	return os.Rename(tmp, filepath.Join(c.QueueDir, name))
}
func readQueue(dir string) ([]queueEntry, error) {
	names, e := os.ReadDir(dir)
	if os.IsNotExist(e) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	var items []queueEntry
	for _, name := range names {
		if name.IsDir() || !strings.HasSuffix(name.Name(), ".json") {
			continue
		}
		b, e := os.ReadFile(filepath.Join(dir, name.Name()))
		if e != nil {
			return nil, e
		}
		var q queuedRequest
		if e = json.Unmarshal(b, &q); e != nil {
			return nil, e
		}
		items = append(items, queueEntry{name.Name(), q})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Filename < items[j].Filename })
	return items, nil
}
func flushQueue(c Config) error {
	items, e := readQueue(c.QueueDir)
	if e != nil {
		return e
	}
	for _, q := range items {
		var body any
		if len(q.Body) > 0 {
			if e = json.Unmarshal(q.Body, &body); e != nil {
				return e
			}
		}
		if _, e = api(c, q.Method, q.Path, body); e != nil {
			return e
		}
		if e = os.Remove(filepath.Join(c.QueueDir, q.Filename)); e != nil {
			return e
		}
	}
	return nil
}
func sendOrQueue(c Config, method, path string, body any, errOut io.Writer) {
	if _, e := api(c, method, path, body); e != nil {
		if q := queue(c, method, path, body); q != nil {
			fmt.Fprintf(errOut, "TaskBridge: request failed: %v; queue failed: %v\n", e, q)
		} else {
			fmt.Fprintf(errOut, "TaskBridge: request queued for retry: %v\n", e)
		}
	}
}
func runTask(c Config, args []string, in io.Reader, out, errOut io.Writer) int {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(errOut)
	name := flags.String("n", "", "task name")
	if e := flags.Parse(args); e != nil {
		return 2
	}
	command := flags.Args()
	if len(command) == 0 {
		fmt.Fprintln(errOut, "usage: tb run -n NAME -- command [args...]")
		return 2
	}
	if *name == "" {
		*name = filepath.Base(command[0])
	}
	if e := flushQueue(c); e != nil {
		fmt.Fprintf(errOut, "TaskBridge: queued requests remain: %v\n", e)
	}
	id := newID()
	host, _ := os.Hostname()
	cwd, _ := os.Getwd()
	start := map[string]any{"id": id, "name": *name, "kind": "terminal", "host": host, "platform": runtime.GOOS, "cwd": cwd, "command": strings.Join(command, " ")}
	sendOrQueue(c, "POST", "/v1/tasks/start", start, errOut)
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Stdin = in
	cmd.Stdout = out
	cmd.Stderr = errOut
	started := time.Now()
	startErr := cmd.Start()
	if startErr != nil {
		fmt.Fprintf(errOut, "TaskBridge: %v\n", startErr)
		sendOrQueue(c, "POST", "/v1/tasks/"+id+"/finish", map[string]any{"exit_code": 127, "error_summary": startErr.Error()}, errOut)
		_ = sendSourceNotification(c, "task:"+id+":finished", ntfyMessage{Title: "❌ " + *name, Message: fmt.Sprintf("Task failed to start\nHost: %s\n%s", host, startErr), Priority: 4, Tags: []string{"x"}}, errOut)
		return 127
	}
	interval := c.HeartbeatSeconds
	if interval <= 0 {
		interval = 120
	}
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var waitErr error
	for {
		select {
		case waitErr = <-done:
			goto finished
		case <-ticker.C:
			_, _ = api(c, "POST", "/v1/tasks/"+id+"/heartbeat", nil)
		}
	}
finished:
	exit := 0
	if waitErr != nil {
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			exit = ee.ExitCode()
		} else {
			exit = 1
		}
		if exit < 0 {
			exit = 1
		}
	}
	finish := map[string]any{"exit_code": exit, "metadata": map[string]any{"elapsed_ms": time.Since(started).Milliseconds()}}
	sendOrQueue(c, "POST", "/v1/tasks/"+id+"/finish", finish, errOut)
	duration := time.Since(started)
	minimum := c.MinSuccessSeconds
	if minimum == 0 {
		minimum = 60
	}
	if minimum < 0 {
		minimum = 0
	}
	if exit != 0 || duration >= time.Duration(minimum)*time.Second {
		message := fmt.Sprintf("Host: %s\nDuration: %s\nExit code: %d", host, duration.Truncate(time.Second), exit)
		notice := ntfyMessage{Title: "✅ " + *name, Message: "Task completed\n" + message, Priority: 3, Tags: []string{"white_check_mark"}}
		if exit != 0 {
			notice = ntfyMessage{Title: "❌ " + *name, Message: "Task failed\n" + message, Priority: 4, Tags: []string{"x"}}
		}
		if e := sendSourceNotification(c, "task:"+id+":finished", notice, errOut); e != nil {
			fmt.Fprintln(errOut, "TaskBridge:", e)
		}
	}
	return exit
}
func initialize(args []string, in io.Reader, out, errOut io.Writer) int {
	f := flag.NewFlagSet("init", flag.ContinueOnError)
	f.SetOutput(errOut)
	endpoint := f.String("url", os.Getenv("TB_URL"), "Worker URL")
	token := f.String("token", os.Getenv("TB_TOKEN"), "client token")
	topic := f.String("ntfy-topic", os.Getenv("TB_NTFY_TOPIC"), "ntfy topic")
	if e := f.Parse(args); e != nil {
		return 2
	}
	reader := bufio.NewReader(in)
	if *token == "" {
		fmt.Fprint(out, "Client token: ")
		line, _ := reader.ReadString('\n')
		*token = strings.TrimSpace(line)
	}
	if *topic == "" {
		fmt.Fprint(out, "ntfy topic: ")
		line, _ := reader.ReadString('\n')
		*topic = strings.TrimSpace(line)
	}
	u, e := url.Parse(*endpoint)
	if e != nil || u.Host == "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"))) {
		fmt.Fprintln(errOut, "invalid Worker URL (HTTPS required)")
		return 2
	}
	if *token == "" {
		fmt.Fprintln(errOut, "token required")
		return 2
	}
	if *topic == "" {
		fmt.Fprintln(errOut, "ntfy topic required")
		return 2
	}
	if e = saveConfig(Config{URL: strings.TrimRight(*endpoint, "/"), Token: *token, NtfyTopic: *topic, HeartbeatSeconds: 120}); e != nil {
		fmt.Fprintln(errOut, e)
		return 1
	}
	fmt.Fprintln(out, "TaskBridge configured:", configPath())
	return 0
}
func execute(args []string, in io.Reader, out, errOut io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(errOut, "usage: tb init|doctor|run|notify|relay|status|retry|hook|mcp")
		return 2
	}
	if args[0] == "init" {
		return initialize(args[1:], in, out, errOut)
	}
	if (args[0] == "hook" || args[0] == "mcp") && os.Getenv("TB_CONFIG") == "" {
		if dir, e := os.UserConfigDir(); e == nil {
			path := filepath.Join(dir, "taskbridge", "codex.json")
			if _, e := os.Stat(path); e == nil {
				_ = os.Setenv("TB_CONFIG", path)
			}
		}
	}
	c, e := loadConfig()
	if e != nil {
		fmt.Fprintf(errOut, "TaskBridge: run tb init first: %v\n", e)
		return 2
	}
	switch args[0] {
	case "hook":
		if len(args) >= 2 && args[1] == "codex" {
			if len(args) >= 3 && args[2] == "event" {
				return handleHook(c, in, errOut)
			}
			if e := configureCodexHook(&c, args[2:]); e != nil {
				fmt.Fprintln(errOut, e)
				return 2
			}
			if e := installCodexHooks(out); e != nil {
				fmt.Fprintln(errOut, e)
				return 1
			}
			return 0
		}
		fmt.Fprintln(errOut, "usage: tb hook codex")
		return 2
	case "mcp":
		serveMCP(c, in, out)
		return 0
	case "clients":
		if len(args) < 3 || args[1] != "create" {
			fmt.Fprintln(errOut, "usage: tb clients create NAME [--scopes=tasks:write,tasks:read,notify:write]")
			return 2
		}
		f := flag.NewFlagSet("clients create", flag.ContinueOnError)
		f.SetOutput(errOut)
		scopes := f.String("scopes", "tasks:write,tasks:read,notify:write,notifications:relay", "comma separated scopes")
		if e := f.Parse(args[3:]); e != nil {
			return 2
		}
		data, e := api(c, "POST", "/v1/clients", map[string]any{"name": args[2], "scopes": strings.Split(*scopes, ",")})
		if e != nil {
			fmt.Fprintln(errOut, e)
			return 1
		}
		fmt.Fprintln(out, string(data))
		return 0
	case "run":
		return runTask(c, args[1:], in, out, errOut)
	case "relay":
		f := flag.NewFlagSet("relay", flag.ContinueOnError)
		f.SetOutput(errOut)
		once := f.Bool("once", false, "claim and publish one batch")
		interval := f.Duration("interval", 20*time.Second, "poll interval")
		if e := f.Parse(args[1:]); e != nil {
			return 2
		}
		if *interval < time.Second {
			fmt.Fprintln(errOut, "interval must be at least one second")
			return 2
		}
		for {
			if e := flushUserCodexInterrupts(); e != nil {
				fmt.Fprintln(errOut, "TaskBridge interrupt relay:", e)
				if *once {
					return 1
				}
			}
			count, e := relayOnce(c)
			if count > 0 {
				fmt.Fprintf(out, "Delivered %d pending notifications\n", count)
			}
			if e != nil {
				fmt.Fprintln(errOut, "TaskBridge relay:", e)
				if *once {
					return 1
				}
			}
			if *once {
				return 0
			}
			time.Sleep(*interval)
		}
	case "retry":
		if e := flushQueue(c); e != nil {
			fmt.Fprintln(errOut, e)
			return 1
		}
		fmt.Fprintln(out, "Queue empty")
		return 0
	case "doctor":
		if e := flushQueue(c); e != nil {
			fmt.Fprintln(errOut, "Queue:", e)
		}
		data, e := api(c, "GET", "/v1/doctor", nil)
		if e != nil {
			fmt.Fprintln(errOut, e)
			return 1
		}
		fmt.Fprintln(out, string(data))
		var report struct {
			Failed int `json:"failed_notification_attempts"`
		}
		if json.Unmarshal(data, &report) == nil && report.Failed > 0 {
			return 1
		}
		return 0
	case "notify":
		f := flag.NewFlagSet("notify", flag.ContinueOnError)
		f.SetOutput(errOut)
		title := f.String("title", "TaskBridge", "title")
		if e := f.Parse(args[1:]); e != nil {
			return 2
		}
		message := strings.Join(f.Args(), " ")
		if message == "" {
			fmt.Fprintln(errOut, "message required")
			return 2
		}
		e = sendSourceNotification(c, "manual:"+newID(), ntfyMessage{Title: *title, Message: message, Priority: 3}, errOut)
		if e != nil {
			fmt.Fprintln(errOut, e)
			return 1
		}
		return 0
	case "status":
		if len(args) < 2 {
			fmt.Fprintln(errOut, "task ID required")
			return 2
		}
		data, e := api(c, "GET", "/v1/tasks/"+url.PathEscape(args[1]), nil)
		if e != nil {
			fmt.Fprintln(errOut, e)
			return 1
		}
		fmt.Fprintln(out, string(data))
		return 0
	default:
		fmt.Fprintf(errOut, "unknown command: %s\n", args[0])
		return 2
	}
}
func main() { os.Exit(execute(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }
