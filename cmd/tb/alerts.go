package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var alertKinds = []string{"success", "failure", "question", "interrupt", "lost", "manual"}

func parsePriority(value string) (int, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	aliases := map[string]int{"min": 1, "silent": 1, "low": 2, "default": 3, "normal": 3, "high": 4, "max": 5, "urgent": 5}
	if level, ok := aliases[value]; ok {
		return level, nil
	}
	if level, err := strconv.Atoi(value); err == nil && level >= 1 && level <= 5 {
		return level, nil
	}
	return 0, fmt.Errorf("invalid priority %q: use 1–5 or min, low, normal, high, urgent", value)
}

func (c Config) priority(kind string) int {
	if level := c.AlertPriorities[kind]; level >= 1 && level <= 5 {
		return level
	}
	switch kind {
	case "failure", "question", "interrupt":
		return 4
	case "lost":
		return 5
	default:
		return 3
	}
}

func priorityFlag(f *flag.FlagSet, name, alias string, fallback int) *int {
	level := fallback
	set := func(value string) error {
		parsed, err := parsePriority(value)
		if err == nil {
			level = parsed
		}
		return err
	}
	help := "ntfy priority: 1–5 or min|low|normal|high|urgent"
	f.Func(name, help, set)
	if alias != "" {
		f.Func(alias, help, set)
	}
	return &level
}

// The ordinary client and Codex have separate credentials, but should share
// user-selected alert levels. TB_CONFIG explicitly limits changes to one file.
func configureAlerts(args []string, out, errOut io.Writer) int {
	f := flag.NewFlagSet("alerts", flag.ContinueOnError)
	f.SetOutput(errOut)
	updates := map[string]int{}
	for _, kind := range alertKinds {
		kind := kind
		f.Func(kind, "priority for "+kind+" notifications (1–5 or min|low|normal|high|urgent)", func(value string) error {
			level, err := parsePriority(value)
			if err == nil {
				updates[kind] = level
			}
			return err
		})
	}
	if err := f.Parse(args); err != nil {
		return 2
	}
	if f.NArg() != 0 {
		fmt.Fprintln(errOut, "usage: tb alerts [--success LEVEL] [--failure LEVEL] [--question LEVEL] [--interrupt LEVEL] [--lost LEVEL] [--manual LEVEL]")
		return 2
	}
	paths := []string{configPath()}
	if os.Getenv("TB_CONFIG") == "" {
		paths = append(paths, filepath.Join(filepath.Dir(paths[0]), "codex.json"))
	}
	type savedConfig struct {
		path   string
		config Config
	}
	var configs []savedConfig
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			fmt.Fprintln(errOut, "TaskBridge:", err)
			return 1
		}
		var c Config
		if err := json.Unmarshal(data, &c); err != nil {
			fmt.Fprintf(errOut, "TaskBridge: parse %s: %v\n", path, err)
			return 1
		}
		if c.AlertPriorities == nil {
			c.AlertPriorities = map[string]int{}
		}
		for kind, level := range updates {
			c.AlertPriorities[kind] = level
		}
		configs = append(configs, savedConfig{path, c})
	}
	if len(configs) == 0 {
		fmt.Fprintln(errOut, "TaskBridge: run tb init first; no client configuration found")
		return 2
	}
	names := []string{"", "min", "low", "normal", "high", "urgent"}
	for _, item := range configs {
		if len(updates) > 0 {
			if err := saveConfigFile(item.path, item.config); err != nil {
				fmt.Fprintln(errOut, "TaskBridge:", err)
				return 1
			}
		}
		fmt.Fprintln(out, item.path)
		for _, kind := range alertKinds {
			level := item.config.priority(kind)
			fmt.Fprintf(out, "  %-9s %d (%s)\n", kind, level, names[level])
		}
	}
	return 0
}
