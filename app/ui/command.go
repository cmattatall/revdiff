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
	active        bool
	input         textinput.Model
	err           string
	selected      int
	history       []string
	searchDraft   string
	historySearch bool
}

type commandEntry struct {
	name        string
	description string
	action      keymap.Action
}

// paletteCommand names user-facing commands independently of keybinding IDs.
// Keyboard motions appear in help but have no command palette entry.
func (m Model) paletteCommand(action keymap.Action) string {
	switch action {
	case keymap.ActionDown, keymap.ActionUp, keymap.ActionPageDown, keymap.ActionPageUp,
		keymap.ActionHalfPageDown, keymap.ActionHalfPageUp, keymap.ActionHome, keymap.ActionEnd,
		keymap.ActionScrollLeft, keymap.ActionScrollRight, keymap.ActionScrollCenter,
		keymap.ActionScrollTop, keymap.ActionScrollBottom,
		keymap.ActionScrollDiffDown, keymap.ActionScrollDiffUp,
		keymap.ActionScrollDiffPageDown, keymap.ActionScrollDiffPageUp,
		keymap.ActionScrollDiffHalfPageDown, keymap.ActionScrollDiffHalfPageUp,
		keymap.ActionNextItem, keymap.ActionPrevItem, keymap.ActionNextHunk, keymap.ActionPrevHunk:
		return ""
	case keymap.ActionNextAnnotation:
		return "annotation next"
	case keymap.ActionPrevAnnotation:
		return "annotation prev"
	case keymap.ActionFlushOutput:
		return "w"
	case keymap.ActionOpenFileInEditor:
		return "edit"
	case keymap.ActionFocusDiff:
		return "focus diff"
	case keymap.ActionFocusTree:
		return "focus tree"
	case keymap.ActionTogglePane:
		return "focus next"
	case keymap.ActionStageHunk, keymap.ActionStageFile:
		verb, scope := "stage", "hunk"
		if m.stagedContext() {
			verb = "unstage"
		}
		if action == keymap.ActionStageFile {
			scope = "file"
		}
		return verb + " " + scope
	case keymap.ActionAnnotateFile:
		return "annotate file"
	case keymap.ActionAnnotList:
		return "annotate list"
	case keymap.ActionToggleCollapsed:
		return "view collapsed"
	case keymap.ActionToggleCompact:
		return "view compact"
	case keymap.ActionToggleWrap:
		return "view wrap"
	case keymap.ActionToggleTree:
		return "view tree"
	case keymap.ActionToggleLineNums:
		return "view numbers"
	case keymap.ActionToggleBlame:
		return "view blame"
	case keymap.ActionToggleWordDiff:
		return "view word diff"
	case keymap.ActionToggleHunk:
		return "hunk toggle"
	case keymap.ActionToggleUntracked:
		return "view untracked"
	case keymap.ActionMarkReviewed:
		return "review mark"
	case keymap.ActionFilterUnreviewed:
		return "filter unreviewed"
	case keymap.ActionFilter:
		return "filter annotated"
	case keymap.ActionThemeSelect:
		return "theme select"
	case keymap.ActionInfo:
		return "review info"
	default:
		return string(action)
	}
}

func (m Model) commandEntries() []commandEntry {
	entries := []commandEntry{
		{"annotate", "annotate the selected hunk or file (:a)", ""},
		{"annotate hunk", "annotate the change hunk under the diff cursor", ""},
		{"q", "quit", keymap.ActionQuit},
		{"harness send", "send annotations to the connected harness", keymap.ActionFlushOutput},
		{"fd", "focus the diff pane", keymap.ActionFocusDiff},
		{"set number", "show line numbers", keymap.ActionToggleLineNums},
		{"set nonumber", "hide line numbers", keymap.ActionToggleLineNums},
		{"set wrap", "enable word wrap", keymap.ActionToggleWrap},
		{"set nowrap", "disable word wrap", keymap.ActionToggleWrap},
	}
	if _, ok := m.tree.(*workingTree); ok {
		entries = append(entries,
			commandEntry{"focus staged", "focus the Staged section", ""},
			commandEntry{"focus changed", "focus the Changes section", ""},
			commandEntry{"fs", "focus the Staged section", ""},
			commandEntry{"fc", "focus the Changes section", ""},
		)
	}
	for _, entry := range m.keymap.Actions() {
		if m.cfg.workingTree && entry.Action == keymap.ActionToggleUntracked {
			continue
		}
		if name := m.paletteCommand(entry.Action); name != "" {
			entries = append(entries, commandEntry{name, entry.Description, entry.Action})
		}
	}
	for name := range m.live.harnesses {
		entries = append(entries, commandEntry{"harness connect " + name, "connect to " + name + " in this directory", ""})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	return entries
}

func (m *Model) startCommand() tea.Cmd {
	m.clearPendingInputState()
	m.nav.scanSeq++
	m.nav.scanKind = treeScanIdle
	ti := textinput.New()
	ti.Prompt = ":"
	ti.Placeholder = "action or line number"
	ti.CharLimit = 128
	ti.Width = max(1, m.layout.width-5) // borders, padding, and ':'
	cmd := ti.Focus()
	m.command = commandState{active: true, input: ti, history: m.command.history}
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
	if m.command.historySearch {
		return m.handleCommandHistoryKey(msg)
	}
	switch msg.Type {
	case tea.KeyCtrlR:
		m.command.historySearch = true
		m.command.searchDraft = m.command.input.Value()
		m.command.selected = 0
		m.command.err = ""
		m.layout.viewport.Height = m.paneHeight() - 1
		return m, nil
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
	if value == "h" {
		value = "help"
	}
	if value == "a" {
		value = "annotate"
	}
	if value == "" {
		m.closeCommand()
		return *m, nil
	}
	if matches := m.commandMatches(); len(matches) == 1 {
		value = matches[0].name
	}
	if name, ok := strings.CutPrefix(value, "harness connect "); ok {
		if m.live.harnesses[name] == nil {
			m.command.err = "Unknown or unavailable harness in this review: " + name
			return *m, nil
		}
		m.command.remember(value)
		m.closeCommand()
		return m.connectHarness(name)
	}
	for _, entry := range m.commandEntries() {
		if entry.name == value {
			if entry.name == "annotate" || entry.name == "annotate file" || entry.name == "annotate hunk" {
				if !m.filesLoaded || m.file.requestedPath != "" {
					m.command.err = "Wait for the selected file to load"
					return *m, nil
				}
				if m.file.name == "" {
					m.command.err = "No file selected"
					return *m, nil
				}
				if _, ok := m.cursorHunkStart(); entry.name == "annotate hunk" && (!ok || m.layout.focus != paneDiff) {
					m.command.err = "Move the diff cursor onto a change hunk"
					return *m, nil
				}
			}
			// Close first: live operations must not see the palette as a modal
			// blocker, and newly opened overlays need the restored pane height.
			m.command.remember(entry.name)
			m.closeCommand()
			// Vim's set/unset commands are idempotent, unlike the underlying
			// toggle actions. Still use normal dispatch when a change is needed.
			switch entry.name {
			case "annotate", "annotate file", "annotate hunk":
				_, onHunk := m.cursorHunkStart()
				hunk := entry.name == "annotate hunk" || (entry.name == "annotate" && m.layout.focus == paneDiff && onHunk)
				m.layout.focus = paneDiff
				var cmd tea.Cmd
				if hunk {
					cmd = m.startHunkAnnotation()
				} else {
					cmd = m.startFileAnnotation()
				}
				m.layout.viewport.SetContent(m.renderDiff())
				return *m, cmd
			case "focus diff", "fd":
				m.layout.focus = paneDiff
				return *m, nil
			case "focus staged", "focus changed", "fs", "fc":
				// These entries exist only for the split working-tree sidebar.
				m.tree.(*workingTree).activeStaged = entry.name == "focus staged" || entry.name == "fs"
				if m.layout.treeHidden {
					m.toggleTreePane()
				}
				m.layout.focus = paneTree
				m.pendingAnnotJump = nil
				m.nav.pendingHunkJump = nil
				return m.loadSelectedIfChanged()
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
	if value != "$" && (err != nil || n < 1 || strings.Trim(value, "0123456789") != "") {
		m.command.err = "Unknown command"
		if strings.Trim(value, "0123456789+-") == "" {
			m.command.err = "Enter a positive line number"
		}
		return *m, nil
	}
	if !m.filesLoaded || m.file.requestedPath != "" {
		m.command.err = "Wait for the selected file to load"
		return *m, nil
	}
	if value == "$" {
		deleted := m.tree.FileStatus(m.file.name) == diff.FileDeleted
		for _, line := range m.file.lines {
			if line.ChangeType == diff.ChangeDivider {
				continue
			}
			number := line.NewNum
			if deleted {
				number = line.OldNum
			}
			n = max(n, number)
		}
		if n == 0 {
			m.command.err = "No source lines are shown"
			return *m, nil
		}
	}
	idx := m.sourceLineIndex(n)
	if idx < 0 {
		m.command.err = fmt.Sprintf("Line %d is not shown", n)
		return *m, nil
	}
	m.command.remember(value)
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
	if query == "$" {
		return nil // source-line address, not a search for $EDITOR commands
	}
	var matches []commandEntry
	for _, entry := range m.commandEntries() {
		// A recalled stage command must not complete to its opposite operation.
		if strings.HasPrefix(query, "stage ") && strings.HasPrefix(entry.name, "unstage ") {
			continue
		}
		// Completing an exact name must not replace it with a substring match
		// (for example, down must not complete to page_down).
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
	if m.command.active && m.command.historySearch {
		return m.commandHistoryRows() + 4 // title, filter, and two borders
	}
	if m.command.active || m.search.active {
		return 4 // input, help/error, and two borders
	}
	return 0
}

func (m Model) commandPaneView() string {
	input := m.command.input
	if m.search.active {
		input = m.search.input
	}
	input.PlaceholderStyle = m.resolver.Style(style.StyleKeyAnnotInputPlaceholder)
	if m.search.active {
		scope := "Search"
		if m.layout.focus == paneTree && m.file.mdTOC == nil {
			scope = "Search file tree"
		}
		return m.inputPaneView(input.View(), scope+" · Enter find · ↑↓ history · Esc cancel")
	}
	if m.command.historySearch {
		return m.commandHistoryView()
	}
	input.CompletionStyle = input.PlaceholderStyle
	help := ""
	if matches := m.commandMatches(); len(matches) > 0 {
		entry := matches[m.command.selected]
		// Suggestions are render-only. Tab accepts the same selected action;
		// moving within the input or scrolling it hides the ghost suffix.
		input.ShowSuggestions = input.Position() == len([]rune(input.Value())) && ansi.StringWidth(input.Value()) < input.Width
		input.SetSuggestions([]string{entry.name})
		help = fmt.Sprintf("%s (%d/%d) · %s",
			entry.name, m.command.selected+1, len(matches), entry.description)
		query := strings.ToLower(strings.TrimSpace(m.command.input.Value()))
		if len(matches) == 1 || query == entry.name || (query == "h" && entry.name == "help") || (query == "a" && entry.name == "annotate") {
			help = entry.description
		}
		if query == "" {
			help = ""
		}
	}
	if m.command.err != "" {
		help = m.command.err
	}
	return m.inputPaneView(input.View(), help)
}

// inputPaneView gives command and search input the same bordered palette pane.
func (m Model) inputPaneView(input, help string) string {
	width := max(0, m.layout.width-4) // borders and horizontal padding
	inputView := ansi.Truncate(input, width, "")
	help = ansi.Truncate(help, width, "…")
	return m.resolver.Style(style.StyleKeyDiffPaneActive).
		Padding(0, 1).Width(max(0, m.layout.width-2)).Render(inputView + "\n" + help)
}
