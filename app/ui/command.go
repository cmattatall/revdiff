package ui

import (
	"fmt"
	"slices"
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

// commandState holds command input, completion, and history.
type commandState struct {
	active        bool
	input         textinput.Model
	err           string
	selected      int
	history       []string
	lastShell     string
	searchDraft   string
	historySearch bool
}

type commandEntry struct {
	name        string
	description string
	aliases     []string
	section     string
}

type paletteCommand interface {
	metadata() commandEntry
	matchesInput(string) bool
	helpName() string
	execute(*Model, string) (tea.Model, tea.Cmd)
}

type tuiCommand struct {
	commandEntry
	action   keymap.Action
	scope    commandScope
	validate func(*Model) string
	run      func(*Model, commandScope) (tea.Model, tea.Cmd)
}

// TODO: Move scope into shared UI state so actions outside the command palette
// can use the same scope and mode rules.
type commandScope int

const (
	commandScopeReview commandScope = iota
	commandScopeFile
	commandScopeHunk
	commandScopeSelection // use the focused hunk, otherwise the selected file
)

func (scope commandScope) resolve(m *Model) (commandScope, string) {
	if scope == commandScopeReview {
		return scope, ""
	}
	if !m.filesLoaded || m.file.requestedPath != "" {
		return scope, "Wait for the selected file to load"
	}
	if m.file.name == "" {
		return scope, "No file selected"
	}
	_, onHunk := m.cursorHunkStart()
	onHunk = onHunk && m.layout.focus == paneDiff
	if scope == commandScopeSelection {
		scope = commandScopeFile
		if onHunk {
			scope = commandScopeHunk
		}
	}
	if scope == commandScopeHunk && !onHunk {
		return scope, "Move the diff cursor onto a change hunk"
	}
	return scope, ""
}

type shellCommand struct {
	commandEntry
	prefix string
}

func (e commandEntry) metadata() commandEntry { return e }

func (e commandEntry) matchesName(name string) bool {
	return e.name == name || slices.Contains(e.aliases, name)
}

func (c tuiCommand) matchesInput(value string) bool {
	return c.matchesName(strings.ToLower(value))
}

func (c shellCommand) arguments(value string) (string, bool) {
	for _, name := range append([]string{c.name}, c.aliases...) {
		if len(value) < len(name) || !strings.EqualFold(value[:len(name)], name) {
			continue
		}
		args := value[len(name):]
		if args == "" || args[0] == ' ' || args[0] == '\t' {
			return args, true
		}
	}
	return "", false
}

func (c shellCommand) matchesInput(value string) bool {
	_, ok := c.arguments(value)
	return ok
}

func (c shellCommand) execute(m *Model, value string) (tea.Model, tea.Cmd) {
	args, _ := c.arguments(value)
	return m.runShellCommand("! " + c.prefix + args)
}

func (e commandEntry) helpName() string {
	name := ":" + e.name
	if len(e.aliases) > 0 {
		name += " (:" + strings.Join(e.aliases, ", :") + ")"
	}
	return name
}

func (c shellCommand) helpName() string {
	entry := c.commandEntry
	entry.name += " <args>"
	return entry.helpName()
}

type commandSetting struct {
	on, off                       string
	onDescription, offDescription string
	action                        keymap.Action
	enabled                       bool
}

func (setting commandSetting) command(enabled bool) tuiCommand {
	name, description := setting.off, setting.offDescription
	if enabled {
		name, description = setting.on, setting.onDescription
	}
	return tuiCommand{
		commandEntry: commandEntry{name: name, description: description, section: "View"},
		action:       setting.action,
		run: func(m *Model, _ commandScope) (tea.Model, tea.Cmd) {
			if setting.action == keymap.ActionToggleCollapsed {
				// Explicit folding also clears individually expanded hunks.
				m.setCollapsedMode(enabled)
				return *m, nil
			}
			if setting.enabled == enabled {
				return *m, nil
			}
			return m.dispatchAction(setting.action)
		},
	}
}

func (m Model) commandSettings() []commandSetting {
	settings := []commandSetting{
		{"set number", "set nonumber", "show line numbers", "hide line numbers", keymap.ActionToggleLineNums, m.modes.lineNumbers},
		{"set wrap", "set nowrap", "wrap long lines", "scroll long lines horizontally", keymap.ActionToggleWrap, m.modes.wrap},
		{"blame on", "blame off", "show the blame gutter", "hide the blame gutter", keymap.ActionToggleBlame, m.modes.showBlame},
		{"diff context compact", "diff context full", "show nearby context around changes", "show the whole file around changes", keymap.ActionToggleCompact, m.modes.compact},
		{"diff removed hide", "diff removed show", "fold removed lines", "expand removed lines", keymap.ActionToggleCollapsed, m.modes.collapsed.enabled},
		{"diff words on", "diff words off", "highlight changed words", "highlight changed lines only", keymap.ActionToggleWordDiff, m.modes.wordDiff},
		{"tree show", "tree hide", "show the file tree pane", "hide the file tree pane", keymap.ActionToggleTree, !m.layout.treeHidden},
	}
	if !m.cfg.workingTree {
		settings = append(settings, commandSetting{"files untracked show", "files untracked hide", "include untracked files", "exclude untracked files", keymap.ActionToggleUntracked, m.modes.showUntracked})
	}
	return settings
}

// paletteCommand names user-facing commands independently of keybinding IDs.
// Keyboard motions appear in help but have no command palette entry.
func (m Model) paletteCommand(action keymap.Action) string {
	for _, setting := range m.commandSettings() {
		if setting.action == action {
			if setting.enabled {
				return setting.off
			}
			return setting.on
		}
	}
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
	case keymap.ActionDeleteAnnotation:
		return "annotation delete"
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
	case keymap.ActionToggleHunk:
		return "hunk toggle"
	case keymap.ActionMarkReviewed:
		return "review mark"
	case keymap.ActionFilterUnreviewed:
		return "filter unreviewed"
	case keymap.ActionFilter:
		return "filter annotated"
	case keymap.ActionThemeSelect:
		return "theme select"
	case keymap.ActionInfo:
		return ""
	default:
		return string(action)
	}
}

func (m Model) commandEntries() []paletteCommand {
	entries := []paletteCommand{
		tuiCommand{commandEntry: commandEntry{name: "annotate", description: "annotate the selected hunk or file", aliases: []string{"a"}, section: "Annotations"}, scope: commandScopeSelection, run: (*Model).annotateScope},
		tuiCommand{commandEntry: commandEntry{name: "annotate hunk", description: "annotate the change hunk under the diff cursor", section: "Annotations"}, scope: commandScopeHunk, run: (*Model).annotateScope},
		tuiCommand{commandEntry: commandEntry{name: "blame view", description: "inspect the current line's commit and associated GitHub PR", aliases: []string{"bv"}, section: "View"},
			run: func(m *Model, _ commandScope) (tea.Model, tea.Cmd) { return m.openBlameView() }},
		tuiCommand{commandEntry: commandEntry{name: "inspect hover", description: "show a symbol's type and documentation", section: "Inspection"},
			run: func(m *Model, _ commandScope) (tea.Model, tea.Cmd) { return m.openInspection(InspectHover) }},
		tuiCommand{commandEntry: commandEntry{name: "inspect definition", description: "preview a symbol's definition", section: "Inspection"},
			run: func(m *Model, _ commandScope) (tea.Model, tea.Cmd) { return m.openInspection(InspectDefinition) }},
		tuiCommand{commandEntry: commandEntry{name: "inspect references", description: "find and preview a symbol's references", section: "Inspection"},
			run: func(m *Model, _ commandScope) (tea.Model, tea.Cmd) { return m.openInspection(InspectReferences) }},
		tuiCommand{commandEntry: commandEntry{name: "lsp list", description: "list language servers and PATH availability", section: "Inspection"},
			run: func(m *Model, _ commandScope) (tea.Model, tea.Cmd) { return m.listLanguageServers() }},
		shellCommand{commandEntry: commandEntry{name: "git", description: "run git through the shell", section: "Miscellaneous"}, prefix: "git"},
		tuiCommand{commandEntry: commandEntry{name: "harness send", description: "compose a message to the connected harness", aliases: []string{"hs"}, section: "Harness"},
			run: func(m *Model, _ commandScope) (tea.Model, tea.Cmd) { return m.openHarnessMessage() }},
		tuiCommand{commandEntry: commandEntry{name: "quit!", description: "discard unsent feedback and quit", aliases: []string{"q!"}, section: "Miscellaneous"},
			validate: func(m *Model) string { return m.quitError(true) },
			run:      func(m *Model, _ commandScope) (tea.Model, tea.Cmd) { return m.quitReview(true) }},
	}
	for language, command := range m.inspection.installCommands {
		entries = append(entries, shellCommand{
			commandEntry: commandEntry{name: "lsp install " + language, description: "install the " + language + " language server", section: "Inspection"},
			prefix:       command,
		})
	}
	settings := m.commandSettings()
	for _, setting := range settings {
		entries = append(entries, setting.command(true), setting.command(false))
	}
	if _, ok := m.tree.(*workingTree); ok {
		entries = append(entries,
			tuiCommand{commandEntry: commandEntry{name: "focus staged", description: "focus the Staged section", aliases: []string{"fs"}, section: keymap.SectionPane},
				run: func(m *Model, _ commandScope) (tea.Model, tea.Cmd) { return m.focusTreeSection(true) }},
			tuiCommand{commandEntry: commandEntry{name: "focus changed", description: "focus the Changes section", aliases: []string{"fc"}, section: keymap.SectionPane},
				run: func(m *Model, _ commandScope) (tea.Model, tea.Cmd) { return m.focusTreeSection(false) }},
		)
	}
actions:
	for _, entry := range m.keymap.Actions() {
		if m.cfg.workingTree && entry.Action == keymap.ActionToggleUntracked {
			continue
		}
		for _, setting := range settings {
			if setting.action == entry.Action {
				continue actions
			}
		}
		if name := m.paletteCommand(entry.Action); name != "" {
			command := tuiCommand{commandEntry: commandEntry{name: name, description: entry.Description, section: entry.Section}, action: entry.Action}
			switch entry.Action {
			case keymap.ActionQuit:
				command.aliases = []string{"q"}
				command.validate = func(m *Model) string { return m.quitError(false) }
			case keymap.ActionHelp:
				command.aliases = []string{"h"}
			case keymap.ActionFocusDiff:
				command.aliases = []string{"fd"}
				command.run = func(m *Model, _ commandScope) (tea.Model, tea.Cmd) {
					m.layout.focus = paneDiff
					return *m, nil
				}
			case keymap.ActionAnnotateFile:
				command.scope, command.run = commandScopeFile, (*Model).annotateScope
			}
			entries = append(entries, command)
		}
	}
	for name := range m.live.harnesses {
		entries = append(entries, tuiCommand{commandEntry: commandEntry{name: "harness connect " + name, description: "connect to " + name + " in this directory", section: "Harness"},
			run: func(m *Model, _ commandScope) (tea.Model, tea.Cmd) { return m.connectHarness(name) }})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].metadata().name < entries[j].metadata().name })
	return entries
}

func (m *Model) focusTreeSection(staged bool) (tea.Model, tea.Cmd) {
	// Only split working-tree reviews register section-focus commands.
	m.tree.(*workingTree).activeStaged = staged
	if m.layout.treeHidden {
		m.toggleTreePane()
	}
	m.layout.focus = paneTree
	m.pendingAnnotJump = nil
	m.nav.pendingHunkJump = nil
	return m.loadSelectedIfChanged()
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
	m.command = commandState{active: true, input: ti, history: m.command.history,
		lastShell: m.command.lastShell}
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
	value := strings.TrimSpace(m.command.input.Value())
	if strings.HasPrefix(value, "!") {
		return m.runShellCommand(value)
	}
	commands := m.commandEntries()
	for _, command := range commands {
		if command.matchesInput(value) {
			return command.execute(m, value)
		}
	}
	value = strings.ToLower(value)
	if value == "" {
		m.closeCommand()
		return *m, nil
	}
	if matches := m.commandMatches(); len(matches) == 1 {
		value = matches[0].name
		for _, command := range commands {
			if command.matchesInput(value) {
				return command.execute(m, value)
			}
		}
	}
	if name, ok := strings.CutPrefix(value, "harness connect "); ok {
		m.command.err = "Unknown or unavailable harness in this review: " + name
		return *m, nil
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

func (entry tuiCommand) execute(m *Model, _ string) (tea.Model, tea.Cmd) {
	scope, err := entry.scope.resolve(m)
	if err == "" && entry.validate != nil {
		err = entry.validate(m)
	}
	if err != "" {
		m.command.err = err
		return *m, nil
	}
	// Record the canonical name for Ctrl+R history, including commands entered by alias.
	m.command.remember(entry.name)
	// Close first so live operations and overlays see the restored pane height.
	m.closeCommand()
	if entry.run != nil {
		return entry.run(m, scope)
	}
	return m.dispatchAction(entry.action)
}

func (m Model) commandMatches() []commandEntry {
	query := strings.ToLower(strings.TrimSpace(m.command.input.Value()))
	if query == "$" || strings.HasPrefix(query, "!") {
		return nil // source addresses and shell text are not command searches
	}
	var matches []commandEntry
	for _, command := range m.commandEntries() {
		entry := command.metadata()
		if command.matchesInput(query) && !entry.matchesName(query) {
			return nil // shell arguments are not completion queries
		}
		// A recalled stage command must not complete to its opposite operation.
		if strings.HasPrefix(query, "stage ") && strings.HasPrefix(entry.name, "unstage ") {
			continue
		}
		// Completing an exact name must not replace it with a substring match
		// (for example, down must not complete to page_down).
		if entry.matchesName(query) {
			return []commandEntry{entry}
		}
		aliasMatch := slices.ContainsFunc(entry.aliases, func(alias string) bool { return strings.HasPrefix(alias, query) })
		if aliasMatch || strings.Contains(entry.name, query) || strings.Contains(strings.ToLower(entry.description), query) {
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
	if m.command.active || m.search.active || m.message.active {
		return 4 // input, help/error, and two borders
	}
	return 0
}

func (m Model) commandPaneView() string {
	if m.message.active {
		return m.harnessMessageView()
	}
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
		if len(matches) == 1 || entry.matchesName(query) {
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
