package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/umputun/revdiff/app/diff"
	"github.com/umputun/revdiff/app/keymap"
	"github.com/umputun/revdiff/app/ui/style"
)

// commandState holds the action palette and Vim-style source-line jump prompt.
type commandState struct {
	active   bool
	input    textinput.Model
	err      string
	selected int
}

type commandEntry struct {
	name        string
	description string
	action      keymap.Action
}

func (m Model) commandEntries() []commandEntry {
	entries := []commandEntry{
		{"q", "quit", keymap.ActionQuit},
		{"q!", "discard and quit (confirms pending annotations)", keymap.ActionDiscardQuit},
		{"w", "flush annotations to output or harness", keymap.ActionFlushOutput},
		{"set number", "show line numbers", keymap.ActionToggleLineNums},
		{"set nonumber", "hide line numbers", keymap.ActionToggleLineNums},
		{"set wrap", "enable word wrap", keymap.ActionToggleWrap},
		{"set nowrap", "disable word wrap", keymap.ActionToggleWrap},
	}
	for _, entry := range m.keymap.Actions() {
		entries = append(entries, commandEntry{string(entry.Action), entry.Description, entry.Action})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	return entries
}

func (m *Model) startCommand() tea.Cmd {
	if !m.filesLoaded || m.file.requestedPath != "" {
		return nil
	}
	m.clearPendingInputState()
	ti := textinput.New()
	ti.Prompt = ":"
	ti.Placeholder = "action or line number"
	ti.CharLimit = 128
	ti.Width = max(1, m.layout.width-5) // borders, padding, and ':'
	cmd := ti.Focus()
	m.command = commandState{active: true, input: ti}
	// The command pane remains independent of --no-status-bar.
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
		return m.submitCommand()
	case tea.KeyUp, tea.KeyDown, tea.KeyTab:
		matches := m.commandMatches()
		if len(matches) == 0 {
			return m, nil
		}
		m.command.err = ""
		switch msg.Type {
		case tea.KeyUp:
			m.command.selected = (m.command.selected + len(matches) - 1) % len(matches)
		case tea.KeyDown:
			m.command.selected = (m.command.selected + 1) % len(matches)
		case tea.KeyTab:
			m.command.input.SetValue(matches[m.command.selected].name)
			m.command.input.CursorEnd()
			m.command.selected = 0
		}
		return m, nil
	default:
		m.command.err = ""
		cmd := m.updateCommandInput(msg)
		return m, cmd
	}
}

// updateCommandInput also handles asynchronous paste replies, not just keys.
func (m *Model) updateCommandInput(msg tea.Msg) tea.Cmd {
	before := m.command.input.Value()
	var cmd tea.Cmd
	m.command.input, cmd = m.command.input.Update(msg)
	if m.command.input.Value() != before {
		m.command.selected = 0
		m.command.err = ""
	}
	return cmd
}

func (m *Model) submitCommand() (tea.Model, tea.Cmd) {
	value := strings.ToLower(strings.TrimSpace(m.command.input.Value()))
	if value == "" {
		m.closeCommand()
		return *m, nil
	}
	for _, entry := range m.commandEntries() {
		if entry.name == value {
			// Close first: live operations must not see the palette as a modal
			// blocker, and newly opened overlays need the restored pane height.
			m.closeCommand()
			// Vim's set/unset commands are idempotent, unlike the underlying
			// toggle actions. Still use normal dispatch when a change is needed.
			switch entry.name {
			case "set number", "set nonumber":
				if m.modes.lineNumbers == (entry.name == "set number") {
					return *m, nil
				}
			case "set wrap", "set nowrap":
				if m.modes.wrap == (entry.name == "set wrap") {
					return *m, nil
				}
			}
			return m.dispatchAction(entry.action)
		}
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 || strings.Trim(value, "0123456789") != "" {
		m.command.err = "Unknown command; Tab completes action names"
		if strings.Trim(value, "0123456789+-") == "" {
			m.command.err = "Enter a positive line number"
		}
		return *m, nil
	}
	idx := m.sourceLineIndex(n)
	if idx < 0 {
		m.command.err = fmt.Sprintf("Line %d is not shown", n)
		return *m, nil
	}
	m.closeCommand()
	m.layout.focus = paneDiff
	m.annot.cursorOnAnnotation = false
	m.nav.diffCursor = idx
	m.ensureHunkExpanded(idx)
	m.syncTOCActiveSection()
	m.centerViewportOnCursor()
	return *m, nil
}

func (m Model) commandMatches() []commandEntry {
	query := strings.ToLower(strings.TrimSpace(m.command.input.Value()))
	var matches []commandEntry
	for _, entry := range m.commandEntries() {
		// Completing an exact name must not replace it with a substring match
		// (for example, quit must never complete to discard_quit).
		if entry.name == query {
			return []commandEntry{entry}
		}
		if strings.Contains(entry.name, query) || strings.Contains(strings.ToLower(entry.description), query) {
			matches = append(matches, entry)
		}
	}
	// Prefer short name prefixes over substring/description matches, so :h
	// suggests help rather than half_page_down or an unrelated description.
	if query != "" {
		sort.SliceStable(matches, func(i, j int) bool {
			a, b := matches[i].name, matches[j].name
			ap, bp := strings.HasPrefix(a, query), strings.HasPrefix(b, query)
			if ap != bp {
				return ap
			}
			return ap && len(a) < len(b)
		})
	}
	return matches
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

func (m Model) commandPaneHeight() int {
	if m.command.active {
		return 4 // input, help/error, and two borders
	}
	return 0
}

func (m Model) commandPaneView() string {
	width := max(0, m.layout.width-4) // borders and horizontal padding
	input := m.command.input
	input.PlaceholderStyle = m.resolver.Style(style.StyleKeyAnnotInputPlaceholder)
	input.CompletionStyle = input.PlaceholderStyle
	help := "Enter run/jump · Tab complete · ↑↓ browse · Esc cancel"
	if matches := m.commandMatches(); len(matches) > 0 {
		entry := matches[m.command.selected]
		// Suggestions are render-only. Tab accepts the same selected action;
		// moving within the input or scrolling it hides the ghost suffix.
		input.ShowSuggestions = input.Position() == len([]rune(input.Value())) && ansi.StringWidth(input.Value()) < input.Width
		input.SetSuggestions([]string{entry.name})
		help = fmt.Sprintf("%s (%d/%d) · Tab complete · ↑↓ browse · %s",
			entry.name, m.command.selected+1, len(matches), entry.description)
		if strings.EqualFold(strings.TrimSpace(m.command.input.Value()), entry.name) {
			help = "Enter run · Esc cancel · " + entry.description
		}
	}
	if m.command.err != "" {
		help = m.command.err
	}
	inputView := ansi.Truncate(input.View(), width, "")
	help = ansi.Truncate(help, width, "…")
	return m.resolver.Style(style.StyleKeyDiffPaneActive).
		Padding(0, 1).Width(max(0, m.layout.width-2)).Render(inputView + "\n" + help)
}
