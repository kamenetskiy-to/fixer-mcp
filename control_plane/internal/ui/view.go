package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/fixer-mcp/control-plane/internal/registry"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	subtleStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	okStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	offStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	cursorStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	disabledStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	helpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
)

const (
	titleWidth = 20
	hintWidth  = 26
)

// View implements tea.Model.
func (m Model) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	header := titleStyle.Render("fixerctl") + subtleStyle.Render("  one entry for the operator's daily utilities")
	if m.version != "" {
		header += subtleStyle.Render("  " + m.version)
	}
	b.WriteString(m.clip(header) + "\n")

	status := fmt.Sprintf("platform: %s · config: %s · %s · %d/%d runnable",
		m.platform(), m.configLabel(), m.marker.Summary(), m.EnabledCount(), len(m.entries))
	b.WriteString(m.clip(subtleStyle.Render(status)) + "\n")

	if m.loadErr != nil {
		b.WriteString(m.clip(errorStyle.Render("reload failed: "+m.loadErr.Error())) + "\n")
	}

	if m.showHelp {
		b.WriteString(m.helpPanel())
	}

	b.WriteString("\n")
	if len(m.entries) == 0 {
		b.WriteString(offStyle.Render("no entries configured") + "\n")
	}
	for i, entry := range m.entries {
		b.WriteString(m.clip(m.renderEntry(i, entry)) + "\n")
	}

	b.WriteString(subtleStyle.Render(strings.Repeat("─", m.separatorWidth())) + "\n")
	if m.last != nil {
		b.WriteString(m.clip(m.renderLastRun()) + "\n")
	}
	b.WriteString(helpStyle.Render("↑/↓ or j/k move · enter run · r reload · ? help · q quit") + "\n")
	return b.String()
}

func (m Model) platform() string {
	if m.goos == "" {
		return "unknown"
	}
	return m.goos
}

func (m Model) configLabel() string {
	if m.configPath == "" {
		return "defaults (PATH)"
	}
	return m.configPath
}

func (m Model) renderEntry(index int, entry registry.Resolved) string {
	cursor := "  "
	if index == m.cursor {
		cursor = cursorStyle.Render("▸ ")
	}

	if entry.Enabled() {
		title := titleStyle.Render(pad(entry.Display(), titleWidth))
		hint := entry.Group()
		return cursor + okStyle.Render("✓ ") + title +
			subtleStyle.Render(" "+pad(hint, hintWidth)) +
			subtleStyle.Render(" "+entry.Binary)
	}

	title := disabledStyle.Render(pad(entry.Display(), titleWidth))
	hint := disabledStyle.Render(pad(entry.Group(), hintWidth))
	return cursor + offStyle.Render("✗ ") + title + " " + hint +
		disabledStyle.Render(" "+entry.Disabled)
}

func (m Model) renderLastRun() string {
	last := m.last
	status := "exit 0"
	style := okStyle
	if last.Err != nil {
		status = last.Err.Error()
		style = errorStyle
	}
	line := fmt.Sprintf("last: %s %s (%.1fs)", last.Display, status, last.Duration.Seconds())
	out := style.Render(line)
	if tail := firstNonEmptyLine(last.Tail); tail != "" {
		out += subtleStyle.Render("  ·  " + tail)
	}
	return out
}

func (m Model) helpPanel() string {
	lines := []string{
		"config: " + m.configLabel(),
		"  path_overrides       entry id -> absolute path or command name",
		"  extra_entries[]      add entries or override a built-in id",
		"  order[]              move entry ids to the top, in order",
		"  vpn_env_file         override the VPN marker path",
		"  repo_root            checkout used by \"fleet check\"",
		"vpn marker: " + m.marker.Path,
		"vpn lanes:  darwin=launchd · linux/wsl=ssh-tunnel",
	}
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(helpStyle.Render(line) + "\n")
	}
	return b.String()
}

func (m Model) separatorWidth() int {
	w := m.width - 1
	if w < 10 {
		w = 60
	}
	return w
}

// clip truncates a rendered line to the terminal width. Styled lines contain
// escape sequences, so clipping is only applied when the plain text would
// overflow; the simple rune count is enough for the menu's fixed layout.
func (m Model) clip(line string) string {
	if m.width <= 0 {
		return line
	}
	// Count visible runes by dropping escape sequences.
	visible := visibleWidth(line)
	if visible <= m.width {
		return line
	}
	return truncate(line, m.width)
}

func visibleWidth(s string) int {
	count := 0
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		switch runes[i] {
		case 0x1b:
			i = skipEscapeSeq(runes, i)
		default:
			count++
		}
	}
	return count
}

func skipEscapeSeq(runes []rune, i int) int {
	if i+1 >= len(runes) {
		return i
	}
	if runes[i+1] == '[' {
		j := i + 2
		for j < len(runes) {
			if runes[j] >= '@' && runes[j] <= '~' {
				return j
			}
			j++
		}
		return len(runes) - 1
	}
	return i + 1
}

func truncate(s string, width int) string {
	runes := []rune(s)
	var b strings.Builder
	visible := 0
	for i := 0; i < len(runes); i++ {
		if runes[i] == 0x1b {
			end := skipEscapeSeq(runes, i)
			b.WriteString(string(runes[i : end+1]))
			i = end
			continue
		}
		if visible >= width {
			break
		}
		b.WriteRune(runes[i])
		visible++
	}
	return b.String()
}

func pad(s string, width int) string {
	runes := []rune(s)
	if len(runes) >= width {
		return string(runes[:width])
	}
	return s + strings.Repeat(" ", width-len(runes))
}

func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}
