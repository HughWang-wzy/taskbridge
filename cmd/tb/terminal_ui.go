package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"
)

var errUICancel = errors.New("cancelled")

func interactiveAvailable(in io.Reader, out io.Writer) bool {
	i, inOK := in.(*os.File)
	o, outOK := out.(*os.File)
	return inOK && outOK && term.IsTerminal(int(i.Fd())) && term.IsTerminal(int(o.Fd())) && os.Getenv("TERM") != "dumb"
}

// Views never write user-provided control sequences to the terminal.
func terminalText(value string) string {
	var b strings.Builder
	for _, r := range value {
		if unicode.IsControl(r) && r != '\n' {
			fmt.Fprintf(&b, "\\x%02x", r)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

type uiScreens interface {
	choose(title, detail string, options []string, selected int) (int, error)
	edit(title, label, value string, multiline bool) (string, error)
	settings(initial map[string]int, detail string) (map[string]int, bool, error)
}
type teaScreens struct {
	in  io.Reader
	out io.Writer
}

func (u teaScreens) run(model tea.Model) (tea.Model, error) {
	return tea.NewProgram(model, tea.WithInput(u.in), tea.WithOutput(u.out), tea.WithAltScreen()).Run()
}
func (u teaScreens) choose(title, detail string, options []string, selected int) (int, error) {
	model, err := u.run(selectionModel{title: title, detail: detail, options: options, selected: selected, height: 24})
	if err != nil {
		return 0, err
	}
	m := model.(selectionModel)
	if !m.accepted {
		return 0, errUICancel
	}
	return m.selected, nil
}
func (u teaScreens) edit(title, label, value string, multi bool) (string, error) {
	model, err := u.run(newEditModel(title, label, value, multi))
	if err != nil {
		return "", err
	}
	m := model.(editModel)
	if !m.accepted {
		return "", errUICancel
	}
	return string(m.text), nil
}
func (u teaScreens) settings(initial map[string]int, detail string) (map[string]int, bool, error) {
	model, err := u.run(newSettingsModel(initial, detail))
	if err != nil {
		return nil, false, err
	}
	m := model.(settingsModel)
	return m.levels, m.accepted, nil
}

func showUIMessage(ui uiScreens, title, message string) {
	_, _ = ui.choose(title, message, []string{"返回 / Back"}, 0)
}
func editAlertSettings(ui uiScreens, out, errOut io.Writer) int {
	configs, err := readAlertConfigs()
	if err != nil {
		fmt.Fprintln(errOut, "TaskBridge:", err)
		return 2
	}
	levels := map[string]int{}
	for _, kind := range alertKinds {
		levels[kind] = configs[0].Config.priority(kind)
	}
	detail := "保存到当前本机配置 / Local settings"
	if len(configs) > 1 {
		detail = "保存时同步本机 CLI 和 Codex 配置 / Apply to CLI + Codex"
	}
	for _, c := range configs[1:] {
		for _, kind := range alertKinds {
			if c.Config.priority(kind) != levels[kind] {
				detail += "\n两份配置当前不同；保存后统一为屏幕所示等级。"
				break
			}
		}
	}
	updated, save, err := ui.settings(levels, detail)
	if errors.Is(err, errUICancel) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(errOut, "TaskBridge:", err)
		return 1
	}
	if !save {
		return 0
	}
	code := saveAlertLevels(updated, out, errOut)
	if code == 0 {
		fmt.Fprintln(out, "Reminder settings saved. Restart an active Codex MCP to apply question defaults.")
	}
	return code
}

func runInteractive(args []string, in io.Reader, out, errOut io.Writer) int {
	if !interactiveAvailable(in, out) {
		fmt.Fprintln(errOut, "tb menu/settings requires an interactive terminal; use tb --help or tb alerts --preset low-noise in scripts.")
		return 2
	}
	ui := teaScreens{in, out}
	if len(args) > 0 && args[0] == "settings" {
		return editAlertSettings(ui, out, errOut)
	}
	c, err := loadConfig()
	if err != nil {
		fmt.Fprintln(errOut, "TaskBridge: run tb init first:", err)
		return 2
	}
	return launchMenu(c, in, out, errOut, ui)
}

func launchMenu(c Config, in io.Reader, out, errOut io.Writer, ui uiScreens) int {
	for {
		cwd, _ := os.Getwd()
		choice, err := ui.choose("任务与提醒 / Tasks & reminders", cwd, []string{"运行任务 / Run task", "常用任务 / Saved tasks", "提醒设置 / Reminder settings", "发送测试提醒 / Test notification", "检查连接 / Check connection", "退出 / Exit"}, 0)
		if errors.Is(err, errUICancel) {
			return 0
		}
		if err != nil {
			fmt.Fprintln(errOut, "TaskBridge:", err)
			return 1
		}
		switch choice {
		case 0:
			code, ran, err := newUITask(c, in, out, errOut, ui)
			if ran {
				return code
			}
			if err != nil && !errors.Is(err, errUICancel) {
				showUIMessage(ui, "无法准备任务 / Task error", err.Error())
			}
		case 1:
			code, ran, err := savedTasksMenu(c, in, out, errOut, ui)
			if ran {
				return code
			}
			if err != nil && !errors.Is(err, errUICancel) {
				showUIMessage(ui, "常用任务 / Saved tasks", err.Error())
			}
		case 2:
			var log, errorsOut strings.Builder
			if code := editAlertSettings(ui, &log, &errorsOut); code != 0 {
				showUIMessage(ui, "设置未保存 / Settings error", errorsOut.String())
			}
			if updated, err := loadConfig(); err == nil {
				c = updated
			}
		case 3:
			n, err := ui.choose("测试提醒 / Test notification", "选择等级；下一步会再次确认发送。", []string{"1 · min", "2 · low", "3 · normal", "4 · high", "5 · urgent", "返回 / Back"}, 5)
			if err != nil || n == 5 {
				continue
			}
			confirm, err := ui.choose("确认发送 / Send?", fmt.Sprintf("将向你的手机发送等级 %d 的测试通知。实际声音由手机决定。", n+1), []string{"发送 / Send", "取消 / Cancel"}, 1)
			if err != nil || confirm != 0 {
				continue
			}
			if err := sendSourceNotification(c, "manual:"+newID(), ntfyMessage{Title: "TaskBridge level test", Message: fmt.Sprintf("Notification priority: %d", n+1), Priority: n + 1}, errOut); err != nil {
				showUIMessage(ui, "发送失败 / Failed", err.Error())
			} else {
				showUIMessage(ui, "测试通知已提交 / Submitted", fmt.Sprintf("等级 %d；请在手机上检查。", n+1))
			}
		case 4:
			var log, errorsOut strings.Builder
			code := execute([]string{"doctor"}, strings.NewReader(""), &log, &errorsOut)
			showUIMessage(ui, fmt.Sprintf("连接检查 / Connection check · %d", code), log.String()+errorsOut.String())
		default:
			return 0
		}
	}
}

func newUITask(c Config, in io.Reader, out, errOut io.Writer, ui uiScreens) (int, bool, error) {
	raw, err := ui.edit("运行任务 / Run task", "粘贴原始命令；保留引号、管道和续行。", "", true)
	if err != nil {
		return 0, false, err
	}
	name, err := ui.edit("任务名称 / Task name", "可直接确认自动名称", inferShellTaskName(raw), false)
	if err != nil {
		return 0, false, err
	}
	if strings.TrimSpace(name) == "" {
		name = inferShellTaskName(raw)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return 0, false, err
	}
	return reviewTask(c, SavedTask{Name: name, Directory: cwd, Command: []string{raw}, Shell: "shell"}, false, in, out, errOut, ui)
}
func savedTasksMenu(c Config, in io.Reader, out, errOut io.Writer, ui uiScreens) (int, bool, error) {
	tasks, err := loadSavedTasks()
	if err != nil {
		return 0, false, err
	}
	var options []string
	for _, task := range tasks {
		options = append(options, task.Key+" · "+task.Name)
	}
	options = append(options, "新增任务 / New task", "返回 / Back")
	n, err := ui.choose("常用任务 / Saved tasks", "运行前可编辑命令、任务名和工作目录。", options, 0)
	if err != nil {
		return 0, false, err
	}
	if n == len(tasks) {
		return newUITask(c, in, out, errOut, ui)
	}
	if n > len(tasks) {
		return 0, false, nil
	}
	return reviewTask(c, tasks[n], true, in, out, errOut, ui)
}

func reviewTask(c Config, task SavedTask, saved bool, in io.Reader, out, errOut io.Writer, ui uiScreens) (int, bool, error) {
	for {
		options := []string{"立即运行 / Run now", "编辑命令 / Edit command", "编辑名称 / Edit name", "编辑工作目录 / Edit directory", "保存到常用任务 / Save favorite"}
		if saved {
			options = append(options, "删除收藏 / Delete favorite")
		}
		options = append(options, "返回，不运行 / Back")
		detail := fmt.Sprintf("名称 Name: %s\n目录 CWD: %s\n\n%s\n\n完成 %d · 失败 %d · 失联 %d", task.Name, task.Directory, displayTaskCommand(task), c.priority("success"), c.priority("failure"), c.priority("lost"))
		n, err := ui.choose("运行前确认 / Review task", detail, options, len(options)-1)
		if errors.Is(err, errUICancel) {
			return 0, false, nil
		}
		if err != nil {
			return 0, false, err
		}
		switch n {
		case 0:
			fmt.Fprintf(out, "TaskBridge · %s\nCWD: %s\n%s\n\n", terminalText(task.Name), terminalText(task.Directory), terminalText(displayTaskCommand(task)))
			return runSavedTask(c, task, nil, in, out, errOut), true, nil
		case 1:
			edited, err := editTaskCommand(task, ui)
			if err == nil {
				task = edited
			} else if !errors.Is(err, errUICancel) {
				return 0, false, err
			}
		case 2:
			name, err := ui.edit("任务名称 / Task name", "修改显示名称", task.Name, false)
			if err == nil && strings.TrimSpace(name) != "" {
				task.Name = strings.TrimSpace(name)
			} else if err != nil && !errors.Is(err, errUICancel) {
				return 0, false, err
			}
		case 3:
			dir, err := ui.edit("工作目录 / Working directory", "修改任务执行目录", task.Directory, false)
			if err == nil {
				absolute, e := filepath.Abs(strings.TrimSpace(dir))
				if e == nil {
					info, e := os.Stat(absolute)
					if e == nil && info.IsDir() {
						task.Directory = absolute
					} else {
						showUIMessage(ui, "无效目录 / Invalid directory", absolute)
					}
				}
			} else if !errors.Is(err, errUICancel) {
				return 0, false, err
			}
		case 4:
			key := task.Key
			if key == "" {
				key = suggestTaskKey(task.Name)
			}
			if !saved {
				key, err = ui.edit("收藏名称 / Favorite key", "字母、数字、下划线或连字符", key, false)
				if err != nil {
					continue
				}
			}
			key = strings.TrimSpace(key)
			if !savedTaskKeyPattern.MatchString(key) {
				showUIMessage(ui, "名称无效 / Invalid key", "Use letters, numbers, _ or -.")
				continue
			}
			tasks, e := loadSavedTasks()
			if e != nil {
				return 0, false, e
			}
			duplicate := false
			for _, other := range tasks {
				if other.Key == key && (!saved || key != task.Key) {
					duplicate = true
				}
			}
			if duplicate {
				confirm, e := ui.choose("替换收藏 / Replace favorite?", key, []string{"替换 / Replace", "取消 / Cancel"}, 1)
				if e != nil || confirm != 0 {
					continue
				}
			}
			task.Key = key
			if err = saveSavedTask(task); err != nil {
				return 0, false, err
			}
			saved = true
		case 5:
			if !saved {
				return 0, false, nil
			}
			confirm, err := ui.choose("删除收藏 / Delete favorite?", "只删除本机收藏记录："+task.Key, []string{"删除 / Delete", "保留 / Keep"}, 1)
			if err == nil && confirm == 0 {
				if err = deleteSavedTask(task.Key); err != nil {
					return 0, false, err
				}
				return 0, false, nil
			}
		default:
			return 0, false, nil
		}
	}
}

func editTaskCommand(task SavedTask, ui uiScreens) (SavedTask, error) {
	if task.Shell != "" {
		value, err := ui.edit("编辑命令 / Edit command", "修改原始 shell 命令", task.Command[0], true)
		if err != nil {
			return task, err
		}
		task.Command = []string{value}
		return task, nil
	}
	args := append([]string(nil), task.Command...)
	for {
		options := make([]string, 0, len(args)+3)
		for i, arg := range args {
			options = append(options, fmt.Sprintf("argv[%d]  %q", i, arg))
		}
		options = append(options, "添加参数 / Add argument", "使用这些参数 / Done", "放弃 / Cancel")
		n, err := ui.choose("编辑命令参数 / Edit argv", "逐个编辑，保留空格、引号和字面值。", options, len(args)+1)
		if err != nil {
			return task, err
		}
		if n == len(args)+2 {
			return task, errUICancel
		}
		if n == len(args)+1 {
			task.Command = args
			return task, nil
		}
		if n == len(args) {
			value, err := ui.edit("添加参数 / Add argument", "一个参数，可包含空格", "", false)
			if err == nil {
				args = append(args, value)
			}
			continue
		}
		action, err := ui.choose("参数 / Argument", fmt.Sprintf("argv[%d] = %q", n, args[n]), []string{"编辑 / Edit", "移除 / Remove", "返回 / Back"}, 0)
		if err != nil {
			continue
		}
		if action == 0 {
			value, err := ui.edit("编辑参数 / Edit argument", "一个参数；不会被 shell 展开", args[n], false)
			if err == nil {
				if n == 0 && strings.TrimSpace(value) == "" {
					continue
				}
				args[n] = value
			}
		} else if action == 1 && n > 0 {
			args = append(args[:n], args[n+1:]...)
		}
	}
}

func suggestTaskKey(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			b.WriteRune(r)
		} else if b.Len() > 0 {
			b.WriteRune('-')
		}
	}
	key := strings.Trim(b.String(), "-")
	if key == "" {
		key = "task"
	}
	return key
}
func displayTaskCommand(task SavedTask) string {
	if task.Shell != "" && len(task.Command) > 0 {
		return task.Command[0]
	}
	parts := make([]string, len(task.Command))
	for i, arg := range task.Command {
		parts[i] = "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
	}
	return strings.Join(parts, " ")
}

// This lexer is only a naming hint. Execution always uses the untouched shell
// text or the original argv, never the tokens returned here.
func inferShellTaskName(raw string) string {
	var words []string
	var token strings.Builder
	var quote rune
	escaped, active := false, false
	flush := func() {
		if active {
			words = append(words, token.String())
			token.Reset()
			active = false
		}
	}
	for _, r := range raw {
		if escaped {
			if r != '\n' {
				token.WriteRune(r)
				active = true
			}
			escaped = false
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				token.WriteRune(r)
			}
			active = true
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			active = true
			continue
		}
		if strings.ContainsRune("|;&", r) {
			flush()
			break
		}
		if unicode.IsSpace(r) {
			flush()
			continue
		}
		token.WriteRune(r)
		active = true
	}
	flush()
	for len(words) > 0 && strings.Contains(words[0], "=") {
		words = words[1:]
	}
	return inferTaskName(words)
}
