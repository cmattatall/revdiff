package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/umputun/revdiff/app/ui/style"
)

const commandHistoryMax = 50

// remember keeps distinct, executed commands in recency order for this session.
func (c *commandState) remember(value string) {
	if i := slices.Index(c.history, value); i >= 0 {
		c.history = slices.Delete(c.history, i, i+1)
	}
	c.history = append(c.history, value)
	if len(c.history) > commandHistoryMax {
		c.history = c.history[len(c.history)-commandHistoryMax:]
	}
}

// historyMatches uses case-insensitive subsequence matching, newest first:
// "vwd" matches "view word diff" without requiring consecutive characters.
func (c commandState) historyMatches() []string {
	query := []rune(strings.ToLower(strings.TrimSpace(c.input.Value())))
	var matches []string
	for i := len(c.history) - 1; i >= 0; i-- {
		pos := 0
		for _, r := range strings.ToLower(c.history[i]) {
			if pos < len(query) && r == query[pos] {
				pos++
			}
		}
		if pos == len(query) {
			matches = append(matches, c.history[i])
		}
	}
	return matches
}

func (m Model) handleCommandHistoryKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	matches := m.command.historyMatches()
	switch msg.Type {
	case tea.KeyCtrlC:
		m.closeCommand()
	case tea.KeyEsc:
		m.command.historySearch = false
		m.command.input.SetValue(m.command.searchDraft)
		m.command.input.CursorEnd()
		m.command.selected = 0
	case tea.KeyEnter, tea.KeyTab:
		if len(matches) > 0 {
			m.command.input.SetValue(matches[m.command.selected])
			m.command.input.CursorEnd()
			m.command.historySearch = false
			m.command.selected = 0
		}
	case tea.KeyCtrlR, tea.KeyDown, tea.KeyUp, tea.KeyPgDown, tea.KeyPgUp:
		if len(matches) > 0 {
			direction := 1
			if msg.Type == tea.KeyPgDown || msg.Type == tea.KeyPgUp {
				direction = min(m.commandHistoryRows(), len(matches))
			}
			if msg.Type == tea.KeyUp || msg.Type == tea.KeyPgUp {
				direction = -direction
			}
			m.command.selected = (m.command.selected + len(matches) + direction) % len(matches)
		}
	default:
		cmd := m.updateCommandInput(msg)
		return m, cmd
	}
	m.layout.viewport.Height = m.paneHeight() - 1
	return m, nil
}

// Keep the picker stable while filtering, with room left for the diff above it.
func (m Model) commandHistoryRows() int {
	return max(1, min(8, len(m.command.history), m.layout.height/2-4))
}

func (m Model) commandHistoryView() string {
	width := max(1, m.layout.width-4)
	matches := m.command.historyMatches()
	rows := m.commandHistoryRows()
	// Center the selection as it moves beyond the visible window. No second
	// cursor or scroll state is needed when filtering or resizing the list.
	start := max(0, min(m.command.selected-rows/2, len(matches)-rows))
	lines := []string{ansi.Truncate(fmt.Sprintf("History (%d/%d)", len(matches), len(m.command.history)), width, "…")}
	for row := range rows {
		i := start + row
		text := ""
		if i < len(matches) {
			prefix := "  "
			if i == m.command.selected {
				prefix = "> "
			}
			text = ansi.Truncate(prefix+matches[i], width, "…")
			if i == m.command.selected {
				text = m.resolver.Style(style.StyleKeyFileSelected).Width(width).Render(text)
			}
		} else if row == 0 {
			text = ansi.Truncate("No matching history", width, "…")
		}
		lines = append(lines, text)
	}
	input := m.command.input
	input.Prompt = "> "
	input.Placeholder = "filter history"
	input.PlaceholderStyle = m.resolver.Style(style.StyleKeyAnnotInputPlaceholder)
	input.Width = max(1, width-2)
	lines = append(lines, ansi.Truncate(input.View(), width, ""))
	return m.resolver.Style(style.StyleKeyDiffPaneActive).
		Padding(0, 1).Width(max(0, m.layout.width-2)).Render(strings.Join(lines, "\n"))
}
