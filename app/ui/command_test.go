package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/revdiff/app/annotation"
	"github.com/umputun/revdiff/app/diff"
	"github.com/umputun/revdiff/app/keymap"
	"github.com/umputun/revdiff/app/ui/mocks"
	"github.com/umputun/revdiff/app/ui/sidepane"
)

func TestModel_CommandSourceLine(t *testing.T) {
	lines := []diff.DiffLine{
		{OldNum: 1, NewNum: 1, Content: "first", ChangeType: diff.ChangeContext},
		{OldNum: 2, Content: "removed", ChangeType: diff.ChangeRemove},
		{OldNum: 3, Content: "also removed", ChangeType: diff.ChangeRemove},
		{NewNum: 2, Content: "replacement", ChangeType: diff.ChangeAdd},
		{OldNum: 4, NewNum: 3, Content: "last", ChangeType: diff.ChangeContext},
	}
	for _, vim := range []bool{false, true} {
		m := testModel([]string{"a.go"}, nil)
		m.file.name, m.file.lines = "a.go", lines
		m.layout.width, m.layout.height = 100, 25
		m.modes.vimMotion = vim
		m.search.term, m.search.matches = "last", []int{4}
		m.annot.cursorOnAnnotation = true
		// Open from the tree as well as the diff: a successful jump focuses the file.
		for _, msg := range []tea.KeyPressMsg{
			{Text: ":"},
			{Text: "2"},
			{Code: tea.KeyEnter},
		} {
			model, _ := m.Update(msg)
			m = model.(Model)
		}
		assert.False(t, m.command.active)
		assert.Equal(t, 3, m.nav.diffCursor, "source line 2 is not diff row 2 or old line 2")
		assert.Equal(t, paneDiff, m.layout.focus)
		assert.False(t, m.annot.cursorOnAnnotation)
		assert.Equal(t, "last", m.search.term, "command must not reuse/clear search state")
		assert.Equal(t, []int{4}, m.search.matches)
	}
}

func TestModel_CommandLastSourceLine(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		m := testModel([]string{"a.go"}, nil)
		status := diff.FileModified
		want := 1
		if deleted {
			status, want = diff.FileDeleted, 2
		}
		m.tree = sidepane.NewFileTree([]diff.FileEntry{{Path: "a.go", Status: status}})
		m.file.name = "a.go"
		m.file.lines = []diff.DiffLine{
			{NewNum: 1, OldNum: 1, Content: "first", ChangeType: diff.ChangeContext},
			{NewNum: 42, OldNum: 55, Content: "last surviving line", ChangeType: diff.ChangeContext},
			{OldNum: 56, Content: "removed tail", ChangeType: diff.ChangeRemove},
			{NewNum: 99, OldNum: 99, ChangeType: diff.ChangeDivider},
		}
		if deleted {
			for i := range 3 {
				m.file.lines[i].NewNum = 0
				m.file.lines[i].ChangeType = diff.ChangeRemove
			}
		}
		m.modes.collapsed.enabled = true
		m.modes.collapsed.expandedHunks = make(map[int]bool)
		m.layout.focus = paneTree
		m.startCommand()
		m.command.input.SetValue("$")
		require.Empty(t, m.commandMatches(), "source addresses must not complete to $EDITOR commands")
		model, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = model.(Model)
		require.False(t, m.command.active)
		require.Equal(t, paneDiff, m.layout.focus)
		require.Equal(t, want, m.nav.diffCursor, "skip dividers and use the correct side's source lines")
		if deleted {
			require.True(t, m.modes.collapsed.expandedHunks[0])
		}
	}
	for _, loading := range []bool{false, true} {
		m := testModel(nil, nil)
		m.filesLoaded = !loading
		m.startCommand()
		m.command.input.SetValue("$")
		model, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = model.(Model)
		require.True(t, m.command.active)
		if loading {
			require.Equal(t, "Wait for the selected file to load", m.command.err)
		} else {
			require.Equal(t, "No source lines are shown", m.command.err)
		}
	}
}

func TestModel_CommandValidation(t *testing.T) {
	for _, tt := range []struct {
		value string
		err   string
	}{
		{"0", "positive line number"},
		{"-1", "positive line number"},
		{"+2", "positive line number"},
		{"unknown", "Unknown command"},
		{"stage", "Unknown command"},
		{"quit now", "Unknown command"},
		{"99999999999999999999", "positive line number"},
		{"2", "Line 2 is not shown"},
		{"200", "Line 200 is not shown"},
	} {
		t.Run(tt.value, func(t *testing.T) {
			m := testModel([]string{"a.go"}, nil)
			m.file.name = "a.go"
			m.file.lines = []diff.DiffLine{
				{NewNum: 1, Content: "first", ChangeType: diff.ChangeContext},
				{OldNum: 2, Content: "deleted", ChangeType: diff.ChangeRemove},
				{NewNum: 2, ChangeType: diff.ChangeDivider},
				{NewNum: 100, Content: "after compact gap", ChangeType: diff.ChangeContext},
			}
			m.layout.width = 100
			m.startCommand()
			model, _ := m.Update(tea.KeyPressMsg{Text: tt.value})
			m = model.(Model)
			model, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = model.(Model)
			assert.True(t, m.command.active, "invalid commands stay editable")
			assert.Equal(t, 0, m.nav.diffCursor)
			assert.Contains(t, ansi.Strip(m.commandPaneView()), tt.err)
			// Correct the input without leaving the prompt.
			model, _ = m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
			m = model.(Model)
			model, _ = m.Update(tea.KeyPressMsg{Text: "100"})
			m = model.(Model)
			model, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = model.(Model)
			assert.False(t, m.command.active)
			assert.Equal(t, 3, m.nav.diffCursor)
		})
	}
}

func TestModel_CommandDeletedFile(t *testing.T) {
	m := testModel([]string{"deleted.go"}, nil)
	m.file.name = "deleted.go"
	m.tree = sidepane.NewFileTree([]diff.FileEntry{{Path: "deleted.go", Status: diff.FileDeleted}})
	m.file.lines = []diff.DiffLine{
		{OldNum: 1, Content: "first", ChangeType: diff.ChangeRemove},
		{OldNum: 2, Content: "second", ChangeType: diff.ChangeRemove},
	}
	m.layout.focus = paneDiff
	m.toggleCollapsedMode()
	m.startCommand()
	m.command.input.SetValue("2")
	m.submitCommand()
	assert.False(t, m.command.active)
	assert.Equal(t, 1, m.nav.diffCursor)
	assert.True(t, m.modes.collapsed.expandedHunks[0], "jump must reveal the deleted line")
}

func TestModel_CommandCompactDeletionIsNotDeletedFile(t *testing.T) {
	m := testModel([]string{"a.go"}, nil)
	m.file.name = "a.go"
	m.file.lines = []diff.DiffLine{{OldNum: 42, Content: "removed", ChangeType: diff.ChangeRemove}}
	m.startCommand()
	m.command.input.SetValue("42")
	m.submitCommand()
	assert.True(t, m.command.active)
	assert.Equal(t, "Line 42 is not shown", m.command.err)
}

func TestModel_CommandModalLifecycle(t *testing.T) {
	for _, exit := range []tea.KeyPressMsg{tea.KeyPressMsg{Code: tea.KeyEsc}, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, tea.KeyPressMsg{Code: tea.KeyEnter}} {
		m := testModel([]string{"a.go"}, nil)
		m.file.name = "a.go"
		m.file.lines = []diff.DiffLine{{OldNum: 1, NewNum: 1, Content: "one", ChangeType: diff.ChangeContext}}
		m.filesLoaded, m.ready, m.file.singleFile = true, true, true
		m.cfg.noStatusBar = true
		m.layout.width, m.layout.height = 80, 24
		m.layout.viewport.SetWidth(78)
		m.layout.viewport.SetHeight(m.paneHeight() - 1)
		originalHeight := m.layout.viewport.Height()
		require.False(t, m.livePaused())
		m.keys.chordPending, m.vim.count, m.vim.leader = "ctrl+w", 3, "g"
		m.startCommand()
		assert.Empty(t, m.keys.chordPending)
		assert.Zero(t, m.vim.count)
		assert.Empty(t, m.vim.leader)
		assert.True(t, m.livePaused())
		assert.Equal(t, originalHeight-4, m.layout.viewport.Height())
		assert.Zero(t, m.statusBarHeight())
		assert.Equal(t, 4, m.commandPaneHeight())
		assert.Contains(t, ansi.Strip(m.View().Content), ":action or line number")
		assert.Equal(t, 24, lipgloss.Height(m.View().Content))
		model, _ := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
		m = model.(Model)
		assert.Zero(t, m.nav.diffCursor)
		assert.True(t, m.command.active)
		model, _ = m.Update(exit)
		m = model.(Model)
		assert.False(t, m.command.active)
		assert.False(t, m.command.input.Focused())
		assert.Equal(t, originalHeight, m.layout.viewport.Height())
		assert.Zero(t, m.statusBarHeight())
		assert.Zero(t, m.commandPaneHeight())
		assert.False(t, m.livePaused())
	}
}

func TestModel_CommandInputEditingAndWidth(t *testing.T) {
	m := testModel([]string{"a.go"}, nil)
	m.file.name = "a.go"
	m.layout.width = 30
	m.startCommand()
	m.command.input.SetValue("1234")
	for _, msg := range []tea.KeyPressMsg{
		{Code: 'a', Mod: tea.ModCtrl},
		{Text: "5"},
		{Code: 'e', Mod: tea.ModCtrl},
		{Code: tea.KeyBackspace},
	} {
		model, _ := m.Update(msg)
		m = model.(Model)
	}
	assert.Equal(t, "5123", m.command.input.Value())
	assert.Equal(t, 4, m.command.input.Position())
	m.command.err = "Enter a positive line number"
	m.file.singleFile = true
	for _, width := range []int{12, 30, 100} {
		model, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		m = model.(Model)
		rendered := m.commandPaneView()
		assert.Equal(t, 4, lipgloss.Height(rendered), "errors must not change pane height")
		for _, line := range strings.Split(rendered, "\n") {
			assert.LessOrEqual(t, ansi.StringWidth(line), width)
		}
		assert.Contains(t, ansi.Strip(rendered), ":5123", "error must not crowd out input")
		assert.Equal(t, 4, m.command.input.Position(), "resize preserves editing position")
	}
}

func TestModel_CommandPaneKeepsSessionFooter(t *testing.T) {
	m := testModel([]string{"a.go"}, nil)
	m.file.name, m.file.singleFile = "a.go", true
	m.review.cfg = &ReviewInfoConfig{VCS: "git", WorkDir: "/work/review"}
	m.live.sender = &feedbackStub{harness: "amp", display: "Review commands T-review"}
	model, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = model.(Model)
	originalHeight := m.layout.viewport.Height()
	m.startCommand()
	m.command.input.SetValue("999")
	m.submitCommand()
	view := ansi.Strip(m.View().Content)
	require.Equal(t, 30, lipgloss.Height(view))
	require.Equal(t, originalHeight-4, m.layout.viewport.Height())
	rows := strings.Split(view, "\n")
	require.Contains(t, rows[25], ":999")
	require.Contains(t, rows[26], "Line 999 is not shown")
	require.Contains(t, rows[28], "Harness (amp): Review commands T-review")
	require.NotContains(t, view, "Repository:")
	require.NotContains(t, rows[29], "Harness")
	require.NotContains(t, view, "connected")
	require.NotContains(t, rows[29], ":999")
	for y := 24; y < 30; y++ {
		require.Equal(t, hitStatus, m.hitTest(5, y), "footer must not map into the diff")
	}
	require.Equal(t, hitNone, m.hitTest(5, 23))
	require.Equal(t, hitDiff, m.hitTest(5, 21))
	m.closeCommand()
	require.Equal(t, originalHeight, m.layout.viewport.Height())
	require.NotContains(t, ansi.Strip(m.View().Content), "Line 999 is not shown")
}

func TestModel_CommandDoesNotStealTextInput(t *testing.T) {
	for _, annotation := range []bool{false, true} {
		m := testModel([]string{"a.go"}, nil)
		m.file.name = "a.go"
		m.layout.focus = paneDiff
		m.file.lines = []diff.DiffLine{{NewNum: 1, Content: "one", ChangeType: diff.ChangeContext}}
		if annotation {
			m.startAnnotation()
		} else {
			m.startSearch()
		}
		model, _ := m.Update(tea.KeyPressMsg{Text: ":"})
		m = model.(Model)
		assert.False(t, m.command.active)
		if annotation {
			assert.Equal(t, ":", m.annot.input.Value())
		} else {
			assert.Equal(t, ":", m.search.input.Value())
		}
	}
}

func TestModel_CommandCompletion(t *testing.T) {
	m := testModel(nil, nil)
	m.startCommand()
	m.command.input.SetValue("STAGE")
	require.Contains(t, ansi.Strip(m.commandPaneView()), "stage file (1/2)")
	for _, key := range []tea.KeyPressMsg{tea.KeyPressMsg{Code: tea.KeyUp}, tea.KeyPressMsg{Code: tea.KeyDown}, tea.KeyPressMsg{Code: tea.KeyDown}} {
		model, _ := m.Update(key)
		m = model.(Model)
	}
	require.Contains(t, ansi.Strip(m.commandPaneView()), "stage hunk (2/2)")
	model, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = model.(Model)
	require.Nil(t, cmd, "ambiguous action names must not execute")
	require.True(t, m.command.active)
	require.Equal(t, "STAGE", m.command.input.Value())
	model, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = model.(Model)
	require.Equal(t, "stage hunk", m.command.input.Value())
	require.Equal(t, len("stage hunk"), m.command.input.Position())
	require.Empty(t, m.command.err)
	// Editing a query resets selection, including when there are no matches.
	model, _ = m.Update(tea.KeyPressMsg{Text: "xyz"})
	m = model.(Model)
	require.Zero(t, m.command.selected)
	model, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = model.(Model)
	require.Equal(t, "stage hunkxyz", m.command.input.Value())
	model, _ = m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m = model.(Model)
	model, _ = m.Update(tea.KeyPressMsg{Text: "wrap long lines"})
	m = model.(Model)
	model, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = model.(Model)
	require.Equal(t, "set wrap", m.command.input.Value(), "descriptions are searchable")
}

func TestModel_CommandSemanticNames(t *testing.T) {
	m := testModel(nil, nil)
	for name, action := range map[string]keymap.Action{
		"diff removed hide": keymap.ActionToggleCollapsed, "diff context compact": keymap.ActionToggleCompact,
		"set wrap": keymap.ActionToggleWrap, "tree show": keymap.ActionToggleTree,
		"set number": keymap.ActionToggleLineNums, "blame on": keymap.ActionToggleBlame,
		"diff words on": keymap.ActionToggleWordDiff, "files untracked show": keymap.ActionToggleUntracked,
		"hunk toggle": keymap.ActionToggleHunk, "review mark": keymap.ActionMarkReviewed,
		"filter unreviewed": keymap.ActionFilterUnreviewed, "filter annotated": keymap.ActionFilter,
		"theme select":    keymap.ActionThemeSelect,
		"annotation next": keymap.ActionNextAnnotation, "annotation prev": keymap.ActionPrevAnnotation,
		"annotation delete": keymap.ActionDeleteAnnotation,
		"focus diff":        keymap.ActionFocusDiff, "focus tree": keymap.ActionFocusTree,
		"focus next": keymap.ActionTogglePane,
		"w":          keymap.ActionFlushOutput,
	} {
		m.startCommand()
		m.command.input.SetValue(name)
		matches := m.commandMatches()
		require.Len(t, matches, 1, name)
		require.Equal(t, name, matches[0].name)
		for _, command := range m.commandEntries() {
			if command.matchesInput(name) {
				tui, ok := command.(tuiCommand)
				require.True(t, ok, name)
				require.Equal(t, action, tui.action, name)
			}
		}
		count := 0
		for _, section := range m.buildHelpSpec().Sections {
			for _, entry := range section.Entries {
				if strings.Split(entry.Command, " (")[0] == ":"+name {
					count++
				}
				require.NotEqual(t, ":"+string(action), entry.Command, "help uses readable names")
			}
		}
		require.Equal(t, 1, count, "help must not duplicate %s", name)
	}
}

func TestModel_CommandDisplaySettings(t *testing.T) {
	for _, focus := range []pane{paneTree, paneDiff} {
		for _, tc := range []struct {
			on, off string
			state   func(Model) bool
		}{
			{"set wrap", "set nowrap", func(m Model) bool { return m.modes.wrap }},
			{"diff words on", "diff words off", func(m Model) bool { return m.modes.wordDiff }},
			{"diff removed hide", "diff removed show", func(m Model) bool { return m.modes.collapsed.enabled }},
			{"diff context compact", "diff context full", func(m Model) bool { return m.modes.compact }},
			{"tree show", "tree hide", func(m Model) bool { return !m.layout.treeHidden }},
			{"files untracked show", "files untracked hide", func(m Model) bool { return m.modes.showUntracked }},
		} {
			m := testModel([]string{"a.go"}, nil)
			m.file.name, m.layout.focus = "a.go", focus
			m.file.lines = []diff.DiffLine{{NewNum: 1, Content: "line", ChangeType: diff.ChangeContext}}
			m.compact.applicable = true
			m.loadUntracked = func() ([]string, error) { return nil, nil }
			for _, enabled := range []bool{false, false, true, true, false} {
				command := tc.off
				if enabled {
					command = tc.on
				}
				m.startCommand()
				m.command.input.SetValue(command)
				model, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				m = model.(Model)
				require.False(t, m.command.active, command)
				require.Equal(t, enabled, tc.state(m), command)
				if tc.on != "tree show" {
					require.Equal(t, focus, m.layout.focus)
				}
			}
		}
	}
}

func TestModel_CommandBlameFromEitherPane(t *testing.T) {
	for _, focus := range []pane{paneTree, paneDiff} {
		for _, staged := range []bool{false, true} {
			m := testModel([]string{"a.go", "b.go"}, nil)
			m.cfg.workingTree = true
			w := newWorkingTree(testFileTreeFactory())
			w.Rebuild([]diff.FileEntry{{Path: "b.go", Staged: !staged}})
			m.tree, m.layout.focus = w, focus
			m.file.name, m.file.staged = "a.go", staged
			m.file.lines = []diff.DiffLine{{OldNum: 1, NewNum: 1, Content: "source", ChangeType: diff.ChangeContext}}
			m.blamer = &mocks.BlamerMock{FileBlameFunc: func(_ string, file string, index bool) (map[int]diff.BlameLine, error) {
				require.Equal(t, "a.go", file, "blame belongs to the displayed diff, not a pending tree selection")
				require.Equal(t, staged, index)
				return map[int]diff.BlameLine{1: {Author: "Reviewer"}}, nil
			}}
			m.startCommand()
			m.command.input.SetValue("blame on")
			model, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = model.(Model)
			require.NotNil(t, cmd)
			model, _ = m.Update(cmd())
			m = model.(Model)
			require.Equal(t, focus, m.layout.focus)
			require.True(t, m.modes.showBlame)
			require.Contains(t, ansi.Strip(m.renderDiff()), "Reviewer")
			m.startCommand()
			m.command.input.SetValue("blame on")
			model, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = model.(Model)
			require.Nil(t, cmd, "enabling an enabled gutter must not refetch blame")
			require.True(t, m.modes.showBlame)
			m.startCommand()
			m.command.input.SetValue("blame off")
			model, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = model.(Model)
			require.Nil(t, cmd)
			require.False(t, m.modes.showBlame)
			require.Equal(t, focus, m.layout.focus)
			require.NotContains(t, ansi.Strip(m.renderDiff()), "Reviewer")
		}
	}
}

func TestModel_CommandFoldAllRemovedLines(t *testing.T) {
	m := testModel([]string{"a.go"}, nil)
	m.file.name = "a.go"
	m.file.lines = []diff.DiffLine{
		{OldNum: 1, Content: "old", ChangeType: diff.ChangeRemove},
		{NewNum: 1, Content: "new", ChangeType: diff.ChangeAdd},
	}
	m.modes.collapsed.enabled = true
	m.modes.collapsed.expandedHunks = map[int]bool{0: true}
	m.startCommand()
	m.command.input.SetValue("diff removed hide")
	model, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = model.(Model)
	require.True(t, m.modes.collapsed.enabled)
	require.Empty(t, m.modes.collapsed.expandedHunks, "explicit hide must also fold individually expanded hunks")
	require.NotContains(t, ansi.Strip(m.renderDiff()), "old")
}

func TestModel_CommandAnnotationNavigationAndWrite(t *testing.T) {
	m := testModel([]string{"a.go"}, nil)
	m.file.name, m.layout.focus = "a.go", paneDiff
	m.file.lines = []diff.DiffLine{
		{NewNum: 2, ChangeType: diff.ChangeAdd},
		{NewNum: 7, ChangeType: diff.ChangeAdd},
		{NewNum: 10, ChangeType: diff.ChangeAdd},
	}
	m.nav.diffCursor = 1
	m.store.Add(annotation.Annotation{File: "a.go", Line: 2, Type: "+", Comment: "first note"})
	m.store.Add(annotation.Annotation{File: "a.go", Line: 10, Type: "+", Comment: "last note"})
	for _, step := range []struct {
		command string
		cursor  int
	}{{"annotation next", 2}, {"annotation prev", 0}} {
		m.startCommand()
		m.command.input.SetValue(step.command)
		model, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = model.(Model)
		require.False(t, m.command.active)
		require.Equal(t, step.cursor, m.nav.diffCursor)
	}
	sender := &feedbackStub{}
	m.live.sender = sender
	m.startCommand()
	m.command.input.SetValue("w")
	model, send := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = model.(Model)
	require.NotNil(t, send)
	model, _ = m.Update(send())
	m = model.(Model)
	require.Zero(t, m.store.Count())
	require.Len(t, sender.content, 1)
	require.Contains(t, sender.content[0], "first note")
	require.Contains(t, sender.content[0], "last note")
}

func TestModel_CommandUniqueCompletionOnEnter(t *testing.T) {
	for _, query := range []string{"set num", "set numb", "show line numbers"} {
		m := testModel([]string{"a.go"}, nil)
		m.file.name = "a.go"
		m.file.lines = []diff.DiffLine{{NewNum: 12, Content: "line", ChangeType: diff.ChangeContext}}
		m.layout.focus = paneTree
		m.startCommand()
		m.command.input.SetValue(query)
		require.Contains(t, ansi.Strip(m.commandPaneView()), "show line numbers")
		require.Equal(t, query, m.command.input.Value(), "rendering must not accept completion")
		model, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = model.(Model)
		require.False(t, m.command.active, query)
		require.True(t, m.modes.lineNumbers, query)
		require.Equal(t, paneTree, m.layout.focus)
	}
	m := testModel(nil, nil)
	m.startCommand()
	m.command.input.SetValue("set n")
	model, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = model.(Model)
	require.Nil(t, cmd)
	require.True(t, m.command.active, "number, nonumber, and nowrap are ambiguous")
	require.False(t, m.modes.lineNumbers)
}

func TestModel_CommandGhostCompletion(t *testing.T) {
	m := testModel(nil, nil)
	m.startCommand()
	model, _ := m.Update(tea.KeyPressMsg{Text: "h"})
	m = model.(Model)
	inputRow := func() string { return strings.Split(ansi.Strip(m.commandPaneView()), "\n")[1] }
	require.Contains(t, inputRow(), ":help", "the help alias is the first suggestion")
	require.Equal(t, "h", m.command.input.Value(), "rendering must not accept the suggestion")
	model, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = model.(Model)
	require.Contains(t, inputRow(), ":help", "an exact alias resolves to one command")
	model, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m = model.(Model)
	require.NotContains(t, inputRow(), ":help", "hide the suffix while editing inside the query")
	model, _ = m.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	m = model.(Model)
	require.Contains(t, inputRow(), ":help")
	model, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = model.(Model)
	model, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = model.(Model)
	require.Equal(t, "help", m.command.input.Value())
	model, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = model.(Model)
	require.True(t, m.overlay.Active())
	require.False(t, m.command.active)
}

func TestModel_CommandAliasCompletion(t *testing.T) {
	m := splitTestModel(t)
	m.startCommand()
	for alias, canonical := range map[string]string{
		"a": "annotate", "h": "help", "q": "quit", "bv": "blame view",
		"hs": "harness send", "fd": "focus diff", "fc": "focus changed", "fs": "focus staged",
	} {
		m.command.input.SetValue(alias)
		matches := m.commandMatches()
		require.Len(t, matches, 1, alias)
		require.Equal(t, canonical, matches[0].name)
		model, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		m = model.(Model)
		require.Equal(t, canonical, m.command.input.Value())
	}
	entry := commandEntry{name: "focus diff", aliases: []string{"fd", "diff"}}
	require.Equal(t, ":focus diff (:fd, :diff)", entry.helpName())
}

func TestModel_CommandVimSettings(t *testing.T) {
	for _, command := range []string{"set number", "set nonumber", "set wrap", "set nowrap"} {
		for _, initial := range []bool{false, true} {
			m := testModel([]string{"a.go"}, nil)
			m.file.name, m.layout.focus = "a.go", paneDiff
			m.file.lines = []diff.DiffLine{{NewNum: 12, Content: "line", ChangeType: diff.ChangeContext}}
			m.modes.lineNumbers, m.modes.wrap = initial, initial
			for range 2 {
				m.startCommand()
				m.command.input.SetValue(command)
				model, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				m = model.(Model)
				require.False(t, m.command.active)
				if strings.Contains(command, "number") {
					require.Equal(t, command == "set number", m.modes.lineNumbers, command)
					require.Equal(t, initial, m.modes.wrap, "number must not change wrap")
				} else {
					require.Equal(t, command == "set wrap", m.modes.wrap, command)
					require.Equal(t, initial, m.modes.lineNumbers, "wrap must not change number")
				}
			}
		}
	}
	m := testModel(nil, nil)
	m.startCommand()
	m.command.input.SetValue("set nu")
	require.Contains(t, strings.Split(ansi.Strip(m.commandPaneView()), "\n")[1], ":set number")
	model, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = model.(Model)
	require.Equal(t, "set number", m.command.input.Value())
}

func TestModel_CommandVimAliases(t *testing.T) {
	for _, command := range []string{"q", "w"} {
		m := testModel(nil, nil)
		m.store.Add(annotation.Annotation{File: "a.go", Line: 1, Comment: "keep this note"})
		sender := &feedbackStub{}
		m.live.sender = sender
		m.startCommand()
		m.command.input.SetValue(command)
		model, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = model.(Model)
		switch command {
		case "q":
			require.Nil(t, cmd)
			require.True(t, m.command.active)
			require.Contains(t, m.command.err, "Unsent")
			require.Equal(t, "q", m.command.input.Value())
			require.Empty(t, m.command.history, "a rejected quit must not enter history")
			require.Equal(t, 1, m.store.Count())
			m.command.input.SetValue("q!")
			model, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = model.(Model)
			require.IsType(t, tea.QuitMsg{}, cmd())
			require.Empty(t, m.store.FormatOutput(), "explicit discard must not emit annotations on exit")
			require.Empty(t, sender.content)
		case "w":
			require.False(t, m.command.active)
			require.NotNil(t, cmd)
			model, _ = m.Update(cmd())
			m = model.(Model)
			require.Equal(t, "Feedback sent", m.output.hint)
			require.Len(t, sender.content, 1)
			require.Contains(t, sender.content[0], "keep this note")
		}
	}
}

func TestModel_CommandHarnessDisconnect(t *testing.T) {
	for _, focus := range []pane{paneTree, paneDiff} {
		sender := &feedbackStub{harness: "example", display: "Review session"}
		m := testNewModel(t, plainRenderer(), annotation.NewStore(), noopHighlighter(), ModelConfig{
			Feedback: sender,
			DiscoverHarnesses: func() ([]FeedbackSender, error) {
				t.Fatal("disconnect must prevent automatic discovery")
				return nil, nil
			},
		})
		m.layout.focus = focus
		m.layout.width = 120
		m.store.Add(annotation.Annotation{File: "a.go", Line: 7, Comment: "keep until send"})
		m.message.draft = "keep my message"
		m.startCommand()
		m.command.input.SetValue("harness dis")
		require.Equal(t, "harness disconnect", m.commandMatches()[0].name)
		model, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = model.(Model)
		require.Nil(t, cmd)
		require.False(t, m.command.active)
		require.Nil(t, m.live.sender)
		require.Equal(t, discoveryDisabled, m.live.discovery)
		require.Equal(t, focus, m.layout.focus)
		require.Equal(t, 1, m.store.Count())
		require.Equal(t, "keep my message", m.message.draft)
		require.Equal(t, []string{"Harness: disconnected"}, m.sessionPanelLines())
		require.Nil(t, m.harnessDiscoveryTick())
		for _, msg := range []tea.Msg{harnessDiscoveryTickMsg{}, liveTickMsg{}} {
			model, cmd = m.Update(msg)
			m = model.(Model)
			require.Nil(t, cmd, "queued timers must not reconnect or refresh")
		}
		model, cmd = m.handleFlushOutput()
		m = model.(Model)
		require.Nil(t, cmd)
		require.Contains(t, m.output.hint, ":harness connect")
		require.Empty(t, sender.content)
	}
}

func TestModel_CommandHarnessConnect(t *testing.T) {
	for _, name := range []string{"amp", "example"} {
		for _, focus := range []pane{paneTree, paneDiff} {
			sender := &feedbackStub{harness: name, display: "Selected session"}
			calls := 0
			m := testNewModel(t, plainRenderer(), annotation.NewStore(), noopHighlighter(), ModelConfig{
				Harnesses: map[string]func() ([]FeedbackSender, error){name: func() ([]FeedbackSender, error) {
					calls++
					return []FeedbackSender{sender}, nil
				}},
			})
			m.layout.focus = focus
			m.store.Add(annotation.Annotation{File: "a.go", Line: 7, Comment: "keep until send"})
			m.startCommand()
			m.command.input.SetValue("harness con")
			require.Equal(t, "harness connect "+name, m.commandMatches()[0].name)
			model, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = model.(Model)
			require.False(t, m.command.active)
			require.Equal(t, discoveryForConnect, m.live.discovery)
			require.Zero(t, calls, "connection IO must run asynchronously")
			require.NotNil(t, cmd)
			model, _ = m.Update(cmd())
			m = model.(Model)
			require.Same(t, sender, m.live.sender)
			require.Equal(t, focus, m.layout.focus)
			require.Equal(t, 1, calls)
			require.Equal(t, 1, m.store.Count())
			require.Empty(t, sender.content, "connecting must never send annotations")
			m.startCommand()
			m.command.input.SetValue("harness connect " + name)
			model, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = model.(Model)
			require.Nil(t, cmd, "a bound session must not be replaced")
			require.Contains(t, m.output.hint, "Already connected")
			require.Equal(t, 1, calls)
		}
	}
	m := testModel(nil, nil)
	m.startCommand()
	m.command.input.SetValue("harness connect unknown")
	model, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = model.(Model)
	require.Nil(t, cmd)
	require.True(t, m.command.active)
	require.Contains(t, m.command.err, "Unknown or unavailable harness")
}

func TestModel_CommandDispatch(t *testing.T) {
	for _, action := range []keymap.Action{keymap.ActionHelp, keymap.ActionSearch, keymap.ActionConfirm,
		keymap.ActionTogglePane, keymap.ActionQuit, keymap.ActionCommand} {
		t.Run(string(action), func(t *testing.T) {
			m := testModel([]string{"a.go"}, nil)
			m.file.name, m.layout.focus = "a.go", paneDiff
			for i := 1; i <= 100; i++ {
				m.file.lines = append(m.file.lines, diff.DiffLine{NewNum: i, Content: "line", ChangeType: diff.ChangeContext})
			}
			m.layout.viewport.SetHeight(20)
			m.layout.viewport.SetContent(m.renderDiff())
			for _, key := range m.keymap.KeysFor(action) {
				m.keymap.Unbind(key)
			}
			m.startCommand()
			command := string(action)
			if action == keymap.ActionTogglePane {
				command = "focus next"
			}
			model, _ := m.Update(tea.KeyPressMsg{Text: command})
			m = model.(Model)
			require.Equal(t, command, m.command.input.Value(), "command names must not be truncated")
			model, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = model.(Model)
			require.Equal(t, action == keymap.ActionCommand, m.command.active)
			switch action {
			case keymap.ActionHelp:
				require.True(t, m.overlay.Active())
			case keymap.ActionSearch:
				require.True(t, m.search.active)
			case keymap.ActionConfirm:
				require.True(t, m.annot.annotating)
			case keymap.ActionTogglePane:
				require.Equal(t, paneTree, m.layout.focus)
			case keymap.ActionQuit:
				require.NotNil(t, cmd)
				require.IsType(t, tea.QuitMsg{}, cmd())
			case keymap.ActionCommand:
				require.Empty(t, m.command.input.Value())
			}
		})
	}
}

func TestModel_CommandUnstage(t *testing.T) {
	for _, command := range []string{"unstage file", "unstage hunk"} {
		for _, annotated := range []bool{false, true} {
			m := splitTestModel(t)
			m.layout.focus = paneDiff
			calls := 0
			m.live.stager = stagerStub{
				unstageFile: func(path, old string) error {
					require.Equal(t, "unstage file", command)
					require.Equal(t, "partial.go", path)
					calls++
					return nil
				},
				unstageHunk: func(path string, lines []diff.DiffLine, cursor int) error {
					require.Equal(t, "unstage hunk", command)
					require.Equal(t, "needle index", lines[cursor].Content)
					calls++
					return nil
				},
			}
			m.startCommand()
			m.command.input.SetValue(strings.TrimPrefix(command, "un"))
			model, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = model.(Model)
			require.Nil(t, cmd, "recalled stage commands must not run the opposite operation")
			require.True(t, m.command.active)
			if annotated {
				m.store.Add(annotation.Annotation{File: "partial.go", Line: 2, Comment: "keep"})
			}
			m.startCommand()
			m.command.input.SetValue(command)
			require.Contains(t, ansi.Strip(m.commandPaneView()), "stage/unstage")
			model, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = model.(Model)
			require.Zero(t, calls)
			if annotated {
				require.Nil(t, cmd)
				require.Contains(t, m.output.hint, "Send or remove annotations for this file before unstaging")
				continue
			}
			require.NotNil(t, cmd)
			require.True(t, cmd().(stagedMsg).unstage)
			require.Equal(t, 1, calls)
		}
	}
}

func TestModel_CommandStage(t *testing.T) {
	for _, action := range []keymap.Action{keymap.ActionStageHunk, keymap.ActionStageFile} {
		for _, annotated := range []bool{false, true} {
			m := testModel([]string{"a.go"}, nil)
			m.tree = sidepane.NewFileTree([]diff.FileEntry{{Path: "a.go", Status: diff.FileModified}})
			m.file.name, m.layout.focus = "a.go", paneDiff
			m.file.lines = []diff.DiffLine{{NewNum: 1, Content: "new", ChangeType: diff.ChangeAdd}}
			calls := 0
			m.live.stager = stagerStub{
				hunk: func(path string, lines []diff.DiffLine, cursor int) error {
					require.Equal(t, keymap.ActionStageHunk, action)
					require.Equal(t, "a.go", path)
					require.Equal(t, "new", lines[cursor].Content)
					calls++
					return nil
				},
				file: func(path, old string) error {
					require.Equal(t, keymap.ActionStageFile, action)
					require.Equal(t, "a.go", path)
					calls++
					return nil
				},
			}
			if annotated {
				m.store.Add(annotation.Annotation{File: "a.go", Line: 1, Comment: "keep"})
			}
			m.startCommand()
			command := "stage hunk"
			if action == keymap.ActionStageFile {
				command = "stage file"
			}
			m.command.input.SetValue(command)
			model, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = model.(Model)
			require.False(t, m.command.active)
			require.Zero(t, calls, "staging must remain asynchronous")
			if annotated {
				require.Nil(t, cmd)
				require.Contains(t, m.output.hint, "Send or remove annotations")
				continue
			}
			require.NotNil(t, cmd, "palette must close before the live-operation guard")
			require.Equal(t, liveStaging, m.live.operation)
			model, reload := m.Update(cmd())
			m = model.(Model)
			require.Equal(t, 1, calls)
			require.NotNil(t, reload)
			require.False(t, m.filesLoaded)
		}
	}
}

func TestModel_CommandWithoutFile(t *testing.T) {
	m := testModel(nil, nil)
	m.startCommand()
	require.True(t, m.command.active)
	m.command.input.SetValue(" QUIT ")
	model, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = model.(Model)
	require.Equal(t, "quit", m.command.input.Value(), "must not complete to discard_quit")
	require.Equal(t, "quit", strings.Trim(strings.Split(ansi.Strip(m.commandPaneView()), "\n")[2], "│ "))
	model, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.False(t, model.(Model).command.active)
	require.NotNil(t, cmd)
	require.IsType(t, tea.QuitMsg{}, cmd())
	m.closeCommand()
	m.filesLoaded = false
	m.startCommand()
	require.True(t, m.command.active)
	require.Contains(t, ansi.Strip(m.View().Content), "action or line number")
}

func TestModel_CommandHelpFromEitherPaneWhileLoading(t *testing.T) {
	for _, focus := range []pane{paneTree, paneDiff} {
		for _, loading := range []string{"none", "file", "tree"} {
			for _, alias := range []string{"h", "help"} {
				m := testModel(nil, nil)
				m.layout.focus = focus
				m.filesLoaded = loading != "tree"
				if loading == "file" {
					m.file.requestedPath = "pending.go"
				}
				for _, key := range []tea.KeyPressMsg{
					{Text: ":"},
					{Text: alias},
					{Code: tea.KeyEnter},
				} {
					model, _ := m.Update(key)
					m = model.(Model)
				}
				require.False(t, m.command.active)
				require.True(t, m.overlay.Active(), "%s from %v during %s load", alias, focus, loading)
				require.Contains(t, ansi.Strip(m.View().Content), "Navigation")
				require.Equal(t, focus, m.layout.focus)
			}
		}
	}
}

func TestModel_CommandFocusSections(t *testing.T) {
	for _, hidden := range []bool{false, true} {
		m := splitTestModel(t)
		w := m.tree.(*workingTree)
		w.Rebuild([]diff.FileEntry{
			{Path: "a.go", Staged: true}, {Path: "partial.go", Staged: true},
			{Path: "a.go"}, {Path: "partial.go"},
		})
		require.True(t, w.staged.SelectByPath("partial.go"))
		require.True(t, w.changes.SelectByPath("partial.go"))
		m.layout.focus = paneDiff
		if hidden {
			m.toggleTreePane()
		}
		for _, target := range []struct {
			command string
			staged  bool
			content string
		}{
			{"focus changed", false, "needle working copy"},
			{"focus staged", true, "needle index"},
		} {
			m.startCommand()
			m.command.input.SetValue(target.command)
			model, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = model.(Model)
			require.False(t, m.command.active)
			require.False(t, m.layout.treeHidden)
			require.Equal(t, paneTree, m.layout.focus)
			require.Equal(t, target.staged, m.selectedTreeStaged())
			require.Equal(t, "partial.go", m.tree.SelectedFile(), "preserve the section's selection, not its first file")
			require.NotNil(t, cmd, "the same path on the other side needs a new diff")
			model, _ = m.Update(cmd())
			m = model.(Model)
			require.Equal(t, target.staged, m.file.staged)
			require.Equal(t, target.content, m.file.lines[len(m.file.lines)-1].Content)

			// A unique prefix runs without Tab, and repeated focus is not a toggle or reload.
			m.startCommand()
			m.command.input.SetValue(target.command[:len(target.command)-2])
			require.Len(t, m.commandMatches(), 1)
			model, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = model.(Model)
			require.False(t, m.command.active)
			require.Nil(t, cmd)
			require.Equal(t, target.staged, m.selectedTreeStaged())
		}
	}
}

func TestModel_CommandFocusAliasesAndEscape(t *testing.T) {
	m := splitTestModel(t)
	w := m.tree.(*workingTree)
	w.Rebuild([]diff.FileEntry{
		{Path: "a.go", Staged: true}, {Path: "partial.go", Staged: true},
		{Path: "a.go"}, {Path: "partial.go"},
	})
	require.True(t, w.staged.SelectByPath("partial.go"))
	require.True(t, w.changes.SelectByPath("partial.go"))
	for _, alias := range []string{"fc", "fs"} {
		for _, command := range []string{alias, "fd"} {
			m.startCommand()
			m.command.input.SetValue(command)
			model, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = model.(Model)
			if cmd != nil {
				model, _ = m.Update(cmd())
				m = model.(Model)
			}
			require.False(t, m.command.active)
			if command == alias {
				require.Equal(t, paneTree, m.layout.focus)
				require.Equal(t, alias == "fs", m.selectedTreeStaged())
			}
		}
		require.Equal(t, paneDiff, m.layout.focus)
		// Escape dismisses the palette before moving focus on the next press.
		m.startCommand()
		model, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
		m = model.(Model)
		require.False(t, m.command.active)
		require.Equal(t, paneDiff, m.layout.focus)
		model, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
		m = model.(Model)
		require.Nil(t, cmd, "returning focus must not reload the diff")
		require.Equal(t, paneTree, m.layout.focus)
		require.Equal(t, alias == "fs", m.selectedTreeStaged())
		require.Equal(t, "partial.go", m.tree.SelectedFile(), "return to the previous selection, not the first file")
	}
}

func TestModel_CommandFocusEmptySectionCancelsLoad(t *testing.T) {
	m := splitTestModel(t)
	w := m.tree.(*workingTree)
	w.changes.Rebuild(nil)
	stale := m.requestFileDiff(m.file.name)()
	m.startCommand()
	m.command.input.SetValue("focus changed")
	model, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = model.(Model)
	require.Nil(t, cmd)
	require.Equal(t, paneTree, m.layout.focus)
	require.False(t, w.activeStaged)
	require.Empty(t, m.file.name)
	model, _ = m.Update(stale)
	m = model.(Model)
	require.Empty(t, m.file.name, "old staged load must not repopulate the empty Changes view")
	require.Empty(t, m.file.lines)
}

func TestModel_CommandFocusDiff(t *testing.T) {
	m := splitTestModel(t)
	m.tree.(*workingTree).activeStaged = false
	model, cmd := m.loadSelectedIfChanged()
	m = model.(Model)
	model, _ = m.Update(cmd())
	m = model.(Model)
	m.nav.diffCursor = 1
	for range 2 {
		m.startCommand()
		m.command.input.SetValue("focus diff")
		model, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = model.(Model)
		require.Nil(t, cmd)
		require.False(t, m.command.active)
		require.Equal(t, paneDiff, m.layout.focus)
		require.Equal(t, 1, m.nav.diffCursor, "focusing must not reset the cursor")
	}
}

func TestModel_CommandFocusTreeAndNext(t *testing.T) {
	m := splitTestModel(t)
	m.layout.focus = paneDiff
	for _, step := range []struct {
		command string
		focus   pane
	}{{"focus tree", paneTree}, {"focus next", paneDiff}, {"focus next", paneTree}, {"fd", paneDiff}} {
		m.startCommand()
		m.command.input.SetValue(step.command)
		model, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = model.(Model)
		require.False(t, m.command.active)
		require.Equal(t, step.focus, m.layout.focus, step.command)
	}
}

func TestModel_CommandFocusWithoutSplitTree(t *testing.T) {
	m := testModel([]string{"a.go"}, nil)
	m.startCommand()
	m.command.input.SetValue("focus staged")
	require.Empty(t, m.commandMatches(), "no Staged section exists in historical/file reviews")
	model, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = model.(Model)
	require.True(t, m.command.active)
	require.Contains(t, m.command.err, "Unknown command")
	m.command.input.SetValue("focus diff")
	model, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = model.(Model)
	require.False(t, m.command.active)
	require.Equal(t, paneDiff, m.layout.focus)
}

func TestTUICommand_Scope(t *testing.T) {
	for _, tc := range []struct {
		name    string
		scope   commandScope
		focus   pane
		cursor  int
		loading bool
		want    commandScope
		blocked bool
	}{
		{"review while loading", commandScopeReview, paneTree, 0, true, commandScopeReview, false},
		{"file while loading", commandScopeFile, paneTree, 0, true, commandScopeFile, true},
		{"hunk on context", commandScopeHunk, paneDiff, 0, false, commandScopeHunk, true},
		{"hunk from tree", commandScopeHunk, paneTree, 1, false, commandScopeHunk, true},
		{"selection from tree", commandScopeSelection, paneTree, 1, false, commandScopeFile, false},
		{"selection on hunk", commandScopeSelection, paneDiff, 1, false, commandScopeHunk, false},
		{"file on hunk", commandScopeFile, paneDiff, 1, false, commandScopeFile, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := testModel([]string{"a.go"}, nil)
			m.file.name, m.layout.focus, m.nav.diffCursor = "a.go", tc.focus, tc.cursor
			m.file.lines = []diff.DiffLine{{NewNum: 1, ChangeType: diff.ChangeContext}, {NewNum: 2, ChangeType: diff.ChangeAdd}}
			if tc.loading {
				m.file.requestedPath = "b.go"
			}
			m.startCommand()
			called := false
			command := tuiCommand{commandEntry: commandEntry{name: "inspect"}, scope: tc.scope,
				run: func(model *Model, scope commandScope) (tea.Model, tea.Cmd) {
					called = true
					require.Equal(t, tc.want, scope)
					require.False(t, model.command.active, "close the palette before invoking handlers")
					require.Equal(t, []string{"inspect"}, model.command.history)
					return *model, nil
				}}
			model, _ := command.execute(&m, "inspect")
			require.Equal(t, !tc.blocked, called)
			require.Equal(t, tc.blocked, model.(Model).command.active)
			if tc.blocked {
				require.NotEmpty(t, model.(Model).command.err)
				require.Empty(t, model.(Model).command.history)
			}
		})
	}
}

func TestTUICommand_RegisteredBehaviorSurvivesRename(t *testing.T) {
	for _, name := range []string{"harness send", "harness connect example", "blame view", "focus diff", "focus staged", "focus changed", "diff removed hide", "quit!"} {
		t.Run(name, func(t *testing.T) {
			m := splitTestModel(t)
			m.layout.focus = paneTree
			m.tree.(*workingTree).activeStaged = name == "focus changed"
			m.modes.collapsed.enabled = false
			m.store.Add(annotation.Annotation{File: "a.go", Line: 1, Comment: "pending"})
			sender := &feedbackStub{}
			m.live.harnesses = map[string]func() ([]FeedbackSender, error){
				"example": func() ([]FeedbackSender, error) { return []FeedbackSender{sender}, nil },
			}
			var registered tuiCommand
			for _, entry := range m.commandEntries() {
				if entry.metadata().name == name {
					registered = entry.(tuiCommand)
					break
				}
			}
			require.Equal(t, name, registered.name)
			registered.name = "renamed"
			m.startCommand()
			model, cmd := registered.execute(&m, "renamed")
			m = model.(Model)
			require.False(t, m.command.active)
			require.Equal(t, []string{"renamed"}, m.command.history)
			switch name {
			case "harness send":
				require.True(t, m.message.active)
			case "harness connect example":
				require.Equal(t, discoveryForConnect, m.live.discovery)
				require.NotNil(t, cmd)
				require.Equal(t, []FeedbackSender{sender}, cmd().(harnessesDiscoveredMsg).sessions)
			case "blame view":
				require.Equal(t, "Focus the diff to inspect line blame", m.keys.hint)
			case "focus diff":
				require.Equal(t, paneDiff, m.layout.focus)
			case "focus staged", "focus changed":
				require.Equal(t, paneTree, m.layout.focus)
				require.Equal(t, name == "focus staged", m.selectedTreeStaged())
			case "diff removed hide":
				require.True(t, m.modes.collapsed.enabled)
			case "quit!":
				require.NotNil(t, cmd)
				require.IsType(t, tea.QuitMsg{}, cmd())
				require.Zero(t, m.store.Count())
			}
		})
	}
}

func TestModel_CommandQuitDuringOperation(t *testing.T) {
	for _, command := range []string{"q", "q!"} {
		m := testModel(nil, nil)
		m.live.operation = liveSending
		m.store.Add(annotation.Annotation{File: "a.go", Line: 1, Comment: "pending"})
		m.startCommand()
		m.command.input.SetValue(command)
		model, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = model.(Model)
		require.Nil(t, cmd)
		require.True(t, m.command.active)
		require.Equal(t, command, m.command.input.Value())
		require.Equal(t, "Wait for the current review operation to finish", m.command.err)
		require.Empty(t, m.command.history)
		require.Equal(t, 1, m.store.Count())
	}
}

func TestModel_CommandAnnotateScopes(t *testing.T) {
	for _, tc := range []struct {
		command           string
		focus             pane
		cursor, line, end int
		change            string
	}{
		{"annotate", paneDiff, 1, 7, 8, "+"},
		{"a", paneDiff, 4, 7, 8, "+"},
		{"annotate hunk", paneDiff, 2, 7, 8, "+"},
		{"annotate hunk", paneDiff, 7, 30, 31, "-"},
		{"annotate", paneDiff, 0, 0, 0, ""},
		{"a", paneTree, 3, 0, 0, ""},
		{"annotate file", paneDiff, 3, 0, 0, ""},
	} {
		m := testModel([]string{"a.go"}, nil)
		m.file.name, m.layout.focus, m.nav.diffCursor = "a.go", tc.focus, tc.cursor
		m.file.lines = []diff.DiffLine{
			{OldNum: 19, NewNum: 6, Content: "context", ChangeType: diff.ChangeContext},
			{OldNum: 20, Content: "old one", ChangeType: diff.ChangeRemove},
			{OldNum: 21, Content: "old two", ChangeType: diff.ChangeRemove},
			{NewNum: 7, Content: "new one", ChangeType: diff.ChangeAdd},
			{NewNum: 8, Content: "new two", ChangeType: diff.ChangeAdd},
			{OldNum: 22, NewNum: 9, Content: "context", ChangeType: diff.ChangeContext},
			{OldNum: 30, Content: "deleted one", ChangeType: diff.ChangeRemove},
			{OldNum: 31, Content: "deleted two", ChangeType: diff.ChangeRemove},
		}
		m.modes.collapsed.enabled = true
		m.modes.collapsed.expandedHunks = make(map[int]bool)
		m.startCommand()
		m.command.input.SetValue(tc.command)
		model, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = model.(Model)
		require.False(t, m.command.active, tc.command)
		require.True(t, m.annot.annotating, tc.command)
		require.Equal(t, paneDiff, m.layout.focus)
		m.annot.input.SetValue("please simplify") // no magic 'hunk' keyword needed
		model, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = model.(Model)
		require.Equal(t, []annotation.Annotation{{File: "a.go", Line: tc.line, EndLine: tc.end, Type: tc.change, Comment: "please simplify"}}, m.store.Get("a.go"))
		require.Zero(t, m.annot.endLine, "range state must not leak into the next annotation")
	}
}

func TestModel_CommandAnnotateCompletionAndValidation(t *testing.T) {
	m := testModel([]string{"a.go"}, nil)
	m.file.name = "a.go"
	var family []string
	for _, entry := range m.commandEntries() {
		if name := entry.metadata().name; strings.HasPrefix(name, "a") {
			family = append(family, name)
		}
	}
	require.Equal(t, []string{"annotate", "annotate file", "annotate hunk", "annotate list", "annotation delete", "annotation next", "annotation prev"}, family)
	m.startCommand()
	m.command.input.SetValue("a")
	require.Equal(t, "annotate", m.commandMatches()[0].name)
	m.command.input.SetValue("annotate hunk")
	model, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = model.(Model)
	require.True(t, m.command.active)
	require.Equal(t, "Move the diff cursor onto a change hunk", m.command.err)
	m.file.requestedPath = "b.go"
	m.command.input.SetValue("annotate")
	model, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = model.(Model)
	require.True(t, m.command.active)
	require.Equal(t, "Wait for the selected file to load", m.command.err)
	m.command.input.SetValue("annotate list")
	model, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = model.(Model)
	require.False(t, m.command.active)
	require.True(t, m.overlay.Active(), "listing annotations does not require a loaded file")
}

func TestModel_CommandOpenEditor(t *testing.T) {
	m := testModel([]string{"a.go"}, nil)
	m.file.name, m.layout.focus = "a.go", paneDiff
	m.file.lines = []diff.DiffLine{{NewNum: 3, Content: "line", ChangeType: diff.ChangeAdd}}
	m.store.Add(annotation.Annotation{File: "a.go", Line: 3, Comment: "first\nsecond", Type: "+"})
	fake := mockEditor("", nil)
	m.editor = fake
	m.startCommand()
	m.command.input.SetValue("open_editor")
	model, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = model.(Model)
	require.False(t, m.command.active)
	require.True(t, m.annot.annotating)
	require.NotNil(t, cmd)
	require.Len(t, fake.CommandCalls(), 1)
	require.Equal(t, "first\nsecond", fake.CommandCalls()[0].Content)
}
