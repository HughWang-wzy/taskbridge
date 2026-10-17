package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var alertKinds = []string{"success", "failure", "question", "interrupt", "lost", "manual"}

var errNoAlertConfig = errors.New("run tb init first; no client configuration found")

type AlertConfig struct {
	Path   string
	Config Config
}

func alertPreset(name string) (map[string]int, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "low-noise", "quiet":
		return map[string]int{"success": 2, "failure": 4, "question": 4, "interrupt": 2, "lost": 5, "manual": 2}, nil
	case "standard", "normal":
		return map[string]int{"success": 3, "failure": 4, "question": 4, "interrupt": 4, "lost": 5, "manual": 3}, nil
	case "important":
		return map[string]int{"success": 4, "failure": 5, "question": 4, "interrupt": 4, "lost": 5, "manual": 4}, nil
	default:
		return nil, fmt.Errorf("invalid alert preset %q: use low-noise, standard, or important", name)
	}
}

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
	var preset map[string]int
	f.Func("preset", "alert preset: low-noise|standard|important (aliases: quiet|normal)", func(value string) error {
		levels, err := alertPreset(value)
		if err == nil {
			preset = levels
		}
		return err
	})
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
		fmt.Fprintln(errOut, "usage: tb alerts [--preset low-noise|standard|important] [--success LEVEL] [--failure LEVEL] [--question LEVEL] [--interrupt LEVEL] [--lost LEVEL] [--manual LEVEL]")
		return 2
	}
	for kind, level := range preset {
		if _, explicit := updates[kind]; !explicit {
			updates[kind] = level
		}
	}
	return saveAlertLevels(updates, out, errOut)
}

func readAlertConfigs() ([]AlertConfig, error) {
	paths := []string{configPath()}
	if os.Getenv("TB_CONFIG") == "" {
		paths = append(paths, filepath.Join(filepath.Dir(paths[0]), "codex.json"))
	}
	var configs []AlertConfig
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var c Config
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		configs = append(configs, AlertConfig{Path: path, Config: c})
	}
	if len(configs) == 0 {
		return nil, errNoAlertConfig
	}
	return configs, nil
}

func saveAlertLevels(levels map[string]int, out, errOut io.Writer) int {
	for kind, level := range levels {
		known := false
		for _, candidate := range alertKinds {
			if candidate == kind {
				known = true
				break
			}
		}
		if !known || level < 1 || level > 5 {
			fmt.Fprintf(errOut, "TaskBridge: invalid alert level %s=%d: use a notification kind and priority 1–5\n", kind, level)
			return 2
		}
	}
	configs, err := readAlertConfigs()
	if err != nil {
		fmt.Fprintln(errOut, "TaskBridge:", err)
		if errors.Is(err, errNoAlertConfig) {
			return 2
		}
		return 1
	}
	names := []string{"", "min", "low", "normal", "high", "urgent"}
	for _, item := range configs {
		if len(levels) > 0 {
			if item.Config.AlertPriorities == nil {
				item.Config.AlertPriorities = map[string]int{}
			}
			for kind, level := range levels {
				item.Config.AlertPriorities[kind] = level
			}
			if err := saveConfigFile(item.Path, item.Config); err != nil {
				fmt.Fprintln(errOut, "TaskBridge:", err)
				return 1
			}
		}
		fmt.Fprintln(out, item.Path)
		for _, kind := range alertKinds {
			level := item.Config.priority(kind)
			fmt.Fprintf(out, "  %-9s %d (%s)\n", kind, level, names[level])
		}
	}
	return 0
}
