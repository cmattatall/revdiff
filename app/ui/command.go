package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/umputun/revdiff/app/diff"
	"github.com/umputun/revdiff/app/ui/style"
)

// commandState holds the Vim-style source-line jump prompt.
type commandState struct {
	active bool
	input  textinput.Model
	err    string
}

func (m *Model) startCommand() tea.Cmd {
	if m.file.name == "" || m.file.requestedPath != "" {
		return nil
	}
	m.clearPendingInputState()
	ti := textinput.New()
	ti.Prompt = ":"
	ti.Placeholder = "line number · Enter to jump · Esc to cancel"
	ti.CharLimit = 20
	ti.Width = max(1, m.layout.width-2)
	cmd := ti.Focus()
	m.command = commandState{active: true, input: ti}
	// The prompt gets a row even with --no-status-bar.
	m.layout.viewport.Height = m.paneHeight() - 1
	return cmd
}

func (m *Model) closeCommand() {
	m.command.active = false
	m.command.input.Blur()
	m.layout.viewport.Height = m.paneHeight() - 1
}

func (m Model) handleCommandKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.closeCommand()
		return m, nil
	case tea.KeyEnter:
		m.submitCommand()
		return m, nil
	default:
		m.command.err = ""
		var cmd tea.Cmd
		m.command.input, cmd = m.command.input.Update(msg)
		return m, cmd
	}
}

func (m *Model) submitCommand() {
	value := strings.TrimSpace(m.command.input.Value())
	if value == "" {
		m.closeCommand()
		return
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 || strings.Trim(value, "0123456789") != "" {
		m.command.err = "Enter a positive line number"
		return
	}
	idx := m.sourceLineIndex(n)
	if idx < 0 {
		m.command.err = fmt.Sprintf("Line %d is not shown", n)
		return
	}
	m.closeCommand()
	m.layout.focus = paneDiff
	m.annot.cursorOnAnnotation = false
	m.nav.diffCursor = idx
	m.ensureHunkExpanded(idx)
	m.syncTOCActiveSection()
	m.centerViewportOnCursor()
}

// sourceLineIndex resolves the new-file gutter number, never the diff row index.
// Only a deleted file uses old-file numbers; a compact deletion-only hunk does not.
func (m Model) sourceLineIndex(n int) int {
	deleted := m.tree.FileStatus(m.file.name) == diff.FileDeleted
	for i, line := range m.file.lines {
		if line.ChangeType == diff.ChangeDivider {
			continue
		}
		number := line.NewNum
		if deleted {
			number = line.OldNum
		}
		if number == n {
			return i
		}
	}
	return -1
}

func (m Model) commandBarText() string {
	suffix := ""
	if m.command.err != "" {
		suffix = "  " + m.command.err
	}
	width := max(0, m.layout.width-m.resolver.Style(style.StyleKeyStatusBar).GetHorizontalFrameSize())
	m.command.input.Width = max(1, width-2-ansi.StringWidth(suffix))
	return ansi.Truncate(m.command.input.View()+suffix, width, "")
}
