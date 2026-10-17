package main

import (
	"fmt"
	"runtime"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
)

func uiFrame(title, body, help string) string {
	return "\n  \x1b[1;36mTaskBridge\x1b[0m  ·  " + terminalText(title) + "\n\n" + body + "\n\n  \x1b[2m" + help + "\x1b[0m\n"
}

type selectionModel struct {
	title, detail                         string
	options                               []string
	selected, height, width, detailOffset int
	accepted, cancelled                   bool
}

func (m selectionModel) Init() tea.Cmd { return nil }
func (m selectionModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch key := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = key.Height
		m.width = key.Width
	case tea.KeyMsg:
		switch key.String() {
		case "ctrl+c", "ctrl+d", "esc", "q":
			m.cancelled = true
			return m, tea.Quit
		case "up", "k":
			m.selected = (m.selected + len(m.options) - 1) % len(m.options)
		case "down", "j", "tab":
			m.selected = (m.selected + 1) % len(m.options)
		case "enter":
			m.accepted = true
			return m, tea.Quit
		case "pgdown":
			m.detailOffset += max(3, m.height/2-2)
		case "pgup":
			m.detailOffset = max(0, m.detailOffset-max(3, m.height/2-2))
		default:
			if !key.Paste && len(key.Runes) == 1 && key.Runes[0] >= '1' && key.Runes[0] <= '9' {
				index := int(key.Runes[0] - '1')
				if index < len(m.options) {
					m.selected = index
					m.accepted = true
					return m, tea.Quit
				}
			}
		}
	}
	return m, nil
}
func (m selectionModel) View() string {
	var b strings.Builder
	detailRows := 0
	if m.detail != "" {
		width := m.width - 4
		if width < 20 {
			width = 76
		}
		lines := wrapUILines(terminalText(m.detail), width)
		detailRows = min(len(lines), max(3, m.height/2-2))
		start := min(m.detailOffset, max(0, len(lines)-detailRows))
		for _, line := range lines[start : start+detailRows] {
			b.WriteString("  " + line + "\n")
		}
		if len(lines) > detailRows {
			fmt.Fprintf(&b, "  [详情 %d–%d / %d · PgUp/PgDn]\n", start+1, start+detailRows, len(lines))
			detailRows++
		}
		b.WriteByte('\n')
	}
	visible := m.height - detailRows - 9
	if visible < 4 {
		visible = 4
	}
	start := 0
	if m.selected >= visible {
		start = m.selected - visible + 1
	}
	if start > 0 {
		b.WriteString("     …\n")
	}
	for i := start; i < len(m.options) && i < start+visible; i++ {
		marker := "  "
		if i == m.selected {
			marker = "› "
		}
		fmt.Fprintf(&b, "  %s%d  %s\n", marker, i+1, terminalText(m.options[i]))
	}
	if start+visible < len(m.options) {
		b.WriteString("     …\n")
	}
	return uiFrame(m.title, b.String(), "↑↓ / j k 选择   Enter 确认   1–9 快捷选择   Esc 返回")
}

func wrapUILines(value string, width int) []string {
	var lines []string
	var line strings.Builder
	cells := 0
	for _, r := range value {
		if r == '\n' {
			lines = append(lines, line.String())
			line.Reset()
			cells = 0
			continue
		}
		w := runewidth.RuneWidth(r)
		if cells+w > width {
			lines = append(lines, line.String())
			line.Reset()
			cells = 0
		}
		line.WriteRune(r)
		cells += w
	}
	return append(lines, line.String())
}

type settingsModel struct {
	levels              map[string]int
	detail              string
	selected, preset    int
	accepted, cancelled bool
}

var presetNames = []string{"low-noise", "standard", "important"}
var presetLabels = []string{"低打扰 / Low noise", "标准 / Standard", "重要任务 / Important"}
var alertLabels = []string{"正常完成 Success", "任务失败 Failure", "需要回答 Question", "任务中断 Interrupt", "任务失联 LOST", "手动通知 Manual"}

func newSettingsModel(initial map[string]int, detail string) settingsModel {
	m := settingsModel{levels: map[string]int{}, detail: detail, preset: -1}
	for _, kind := range alertKinds {
		m.levels[kind] = initial[kind]
	}
	for index, name := range presetNames {
		levels, _ := alertPreset(name)
		same := true
		for _, kind := range alertKinds {
			if levels[kind] != m.levels[kind] {
				same = false
			}
		}
		if same {
			m.preset = index
		}
	}
	return m
}
func (m settingsModel) Init() tea.Cmd { return nil }
func (m settingsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "ctrl+c", "ctrl+d", "esc", "q":
		m.cancelled = true
		return m, tea.Quit
	case "up", "k":
		m.selected = (m.selected + len(alertKinds)) % (len(alertKinds) + 1)
	case "down", "j", "tab":
		m.selected = (m.selected + 1) % (len(alertKinds) + 1)
	case "left", "h", "right", "l":
		delta := 1
		if key.String() == "left" || key.String() == "h" {
			delta = -1
		}
		if m.selected == 0 {
			if m.preset < 0 {
				if delta > 0 {
					m.preset = 0
				} else {
					m.preset = len(presetNames) - 1
				}
			} else {
				m.preset = (m.preset + delta + len(presetNames)) % len(presetNames)
			}
			m.levels, _ = alertPreset(presetNames[m.preset])
		} else {
			kind := alertKinds[m.selected-1]
			level := m.levels[kind] + delta
			if level >= 1 && level <= 5 {
				m.levels[kind] = level
				m.preset = -1
			}
		}
	case "enter":
		m.accepted = true
		return m, tea.Quit
	}
	return m, nil
}
func (m settingsModel) View() string {
	var b strings.Builder
	if m.detail != "" {
		b.WriteString("  " + terminalText(m.detail) + "\n\n")
	}
	preset := "自定义 / Custom"
	if m.preset >= 0 {
		preset = presetLabels[m.preset]
	}
	marker := "  "
	if m.selected == 0 {
		marker = "› "
	}
	fmt.Fprintf(&b, "  %s提醒方案 Preset       ‹ %s ›\n\n", marker, preset)
	names := []string{"", "min", "low", "normal", "high", "urgent"}
	for i, kind := range alertKinds {
		marker = "  "
		if m.selected == i+1 {
			marker = "› "
		}
		fmt.Fprintf(&b, "  %s%-24s ‹ %d · %s ›\n", marker, alertLabels[i], m.levels[kind], names[m.levels[kind]])
	}
	b.WriteString("\n  等级由 ntfy/手机决定声音；urgent 不保证绕过静音或专注模式。")
	return uiFrame("提醒设置 / Reminder settings", b.String(), "↑↓ 选择   ←→ 调整   Enter 保存   Esc 放弃修改")
}

type editModel struct {
	title, label                   string
	text                           []rune
	cursor, width, height          int
	multiline, accepted, cancelled bool
	err                            string
}

func newEditModel(title, label, value string, multiline bool) editModel {
	return editModel{title: title, label: label, text: []rune(value), cursor: len([]rune(value)), width: 80, height: 24, multiline: multiline}
}
func (m editModel) Init() tea.Cmd { return nil }
func (m *editModel) insert(value []rune) {
	updated := make([]rune, 0, len(m.text)+len(value))
	updated = append(updated, m.text[:m.cursor]...)
	updated = append(updated, value...)
	updated = append(updated, m.text[m.cursor:]...)
	m.text = updated
	m.cursor += len(value)
}
func (m editModel) lineBounds() (int, int) {
	start, end := m.cursor, m.cursor
	for start > 0 && m.text[start-1] != '\n' {
		start--
	}
	for end < len(m.text) && m.text[end] != '\n' {
		end++
	}
	return start, end
}
func (m editModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch key := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = key.Width, key.Height
	case tea.KeyMsg:
		m.err = ""
		switch key.Type {
		case tea.KeyEsc, tea.KeyCtrlC, tea.KeyCtrlD:
			m.cancelled = true
			return m, tea.Quit
		case tea.KeyEnter, tea.KeyCtrlJ:
			_, end := m.lineBounds()
			continuation := '\\'
			if runtime.GOOS == "windows" {
				continuation = '^'
			}
			continued := m.multiline && m.cursor == len(m.text) && end > 0 && m.text[end-1] == continuation
			if m.multiline && (key.Type == tea.KeyCtrlJ || key.Alt || continued) {
				m.insert([]rune{'\n'})
				break
			}
			if m.multiline && strings.TrimSpace(string(m.text)) == "" {
				m.err = "请输入命令 / Enter a command"
				break
			}
			m.accepted = true
			return m, tea.Quit
		case tea.KeyLeft:
			if m.cursor > 0 {
				m.cursor--
			}
		case tea.KeyRight:
			if m.cursor < len(m.text) {
				m.cursor++
			}
		case tea.KeyHome, tea.KeyCtrlA:
			m.cursor, _ = m.lineBounds()
		case tea.KeyEnd, tea.KeyCtrlE:
			_, m.cursor = m.lineBounds()
		case tea.KeyUp:
			start, _ := m.lineBounds()
			column := m.cursor - start
			if start > 0 {
				previous := start - 1
				for previous > 0 && m.text[previous-1] != '\n' {
					previous--
				}
				m.cursor = previous + min(column, start-1-previous)
			}
		case tea.KeyDown:
			start, end := m.lineBounds()
			column := m.cursor - start
			if end < len(m.text) {
				next := end + 1
				finish := next
				for finish < len(m.text) && m.text[finish] != '\n' {
					finish++
				}
				m.cursor = next + min(column, finish-next)
			}
		case tea.KeyBackspace, tea.KeyCtrlH:
			if m.cursor > 0 {
				m.text = append(m.text[:m.cursor-1], m.text[m.cursor:]...)
				m.cursor--
			}
		case tea.KeyDelete:
			if m.cursor < len(m.text) {
				m.text = append(m.text[:m.cursor], m.text[m.cursor+1:]...)
			}
		case tea.KeyCtrlU:
			m.text = nil
			m.cursor = 0
		case tea.KeyCtrlK:
			_, end := m.lineBounds()
			m.text = append(m.text[:m.cursor], m.text[end:]...)
		case tea.KeySpace:
			m.insert([]rune{' '})
		case tea.KeyRunes:
			value := strings.ReplaceAll(string(key.Runes), "\r\n", "\n")
			value = strings.ReplaceAll(value, "\r", "\n")
			if !m.multiline {
				value = strings.ReplaceAll(value, "\n", " ")
			}
			valid := true
			for _, r := range value {
				if unicode.IsControl(r) && r != '\n' && r != '\t' {
					valid = false
				}
			}
			if valid {
				m.insert([]rune(value))
			} else {
				m.err = "粘贴内容包含控制字符 / Unsupported control character"
			}
		}
	}
	return m, nil
}
func (m editModel) View() string {
	// A visible caret keeps long/multiline editing usable without guessing the
	// terminal's Unicode cursor position. Bubble Tea owns the real cursor.
	value := string(m.text[:m.cursor]) + "▏" + string(m.text[m.cursor:])
	width := max(20, m.width-6)
	var lines []string
	var line strings.Builder
	cells, cursorLine := 0, 0
	for _, r := range value {
		if r == '\n' {
			lines = append(lines, line.String())
			line.Reset()
			cells = 0
			continue
		}
		text := terminalText(string(r))
		w := runewidth.StringWidth(text)
		if cells+w > width {
			lines = append(lines, line.String())
			line.Reset()
			cells = 0
		}
		if r == '▏' {
			cursorLine = len(lines)
		}
		line.WriteString(text)
		cells += w
	}
	lines = append(lines, line.String())
	count := max(3, m.height-12)
	start := max(0, cursorLine-count+1)
	end := min(len(lines), start+count)
	var b strings.Builder
	b.WriteString("  " + terminalText(m.label) + "\n\n")
	if start > 0 {
		b.WriteString("  …\n")
	}
	for _, v := range lines[start:end] {
		b.WriteString("  " + v + "\n")
	}
	if end < len(lines) {
		b.WriteString("  …\n")
	}
	if m.err != "" {
		b.WriteString("\n  " + m.err)
	}
	help := "←→ 移动   Home/End   Ctrl+U 清空   Enter 确认   Esc 返回"
	if m.multiline {
		help += "\n  支持多行粘贴 · Ctrl+J 换行 · 续行符后 Enter 换行"
	}
	return uiFrame(m.title, b.String(), help)
}
