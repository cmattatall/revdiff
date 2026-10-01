package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/revdiff/app/diff"
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
		for _, msg := range []tea.KeyMsg{
			{Type: tea.KeyRunes, Runes: []rune(":")},
			{Type: tea.KeyRunes, Runes: []rune("2")},
			{Type: tea.KeyEnter},
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

func TestModel_CommandValidation(t *testing.T) {
	for _, tt := range []struct {
		value string
		err   string
	}{
		{"0", "positive line number"},
		{"-1", "positive line number"},
		{"+2", "positive line number"},
		{"q", "positive line number"},
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
			model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.value)})
			m = model.(Model)
			model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			m = model.(Model)
			assert.True(t, m.command.active, "invalid commands stay editable")
			assert.Equal(t, 0, m.nav.diffCursor)
			assert.Contains(t, ansi.Strip(m.commandPaneView()), tt.err)
			// Correct the input without leaving the prompt.
			model, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
			m = model.(Model)
			model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("100")})
			m = model.(Model)
			model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
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
	for _, exit := range []tea.KeyType{tea.KeyEsc, tea.KeyCtrlC, tea.KeyEnter} {
		m := testModel([]string{"a.go"}, nil)
		m.file.name = "a.go"
		m.file.lines = []diff.DiffLine{{OldNum: 1, NewNum: 1, Content: "one", ChangeType: diff.ChangeContext}}
		m.filesLoaded, m.ready, m.file.singleFile = true, true, true
		m.cfg.noStatusBar = true
		m.layout.width, m.layout.height = 80, 24
		m.layout.viewport.Width = 78
		m.layout.viewport.Height = m.paneHeight() - 1
		originalHeight := m.layout.viewport.Height
		require.False(t, m.livePaused())
		m.keys.chordPending, m.vim.count, m.vim.leader = "ctrl+w", 3, "g"
		m.startCommand()
		assert.Empty(t, m.keys.chordPending)
		assert.Zero(t, m.vim.count)
		assert.Empty(t, m.vim.leader)
		assert.True(t, m.livePaused())
		assert.Equal(t, originalHeight-4, m.layout.viewport.Height)
		assert.Zero(t, m.statusBarHeight())
		assert.Equal(t, 4, m.commandPaneHeight())
		assert.Contains(t, ansi.Strip(m.View()), ":line number")
		assert.Equal(t, 24, lipgloss.Height(m.View()))
		model, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
		m = model.(Model)
		assert.Zero(t, m.nav.diffCursor)
		assert.True(t, m.command.active)
		model, _ = m.Update(tea.KeyMsg{Type: exit})
		m = model.(Model)
		assert.False(t, m.command.active)
		assert.False(t, m.command.input.Focused())
		assert.Equal(t, originalHeight, m.layout.viewport.Height)
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
	for _, msg := range []tea.KeyMsg{
		{Type: tea.KeyCtrlA},
		{Type: tea.KeyRunes, Runes: []rune("5")},
		{Type: tea.KeyCtrlE},
		{Type: tea.KeyBackspace},
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
	originalHeight := m.layout.viewport.Height
	m.startCommand()
	m.command.input.SetValue("999")
	m.submitCommand()
	view := ansi.Strip(m.View())
	require.Equal(t, 30, lipgloss.Height(view))
	require.Equal(t, originalHeight-4, m.layout.viewport.Height)
	rows := strings.Split(view, "\n")
	require.Contains(t, rows[24], ":999")
	require.Contains(t, rows[25], "Line 999 is not shown")
	require.Contains(t, rows[27], "Harness (amp): Review commands T-review")
	require.Contains(t, rows[28], "Repository: /work/review")
	require.NotContains(t, rows[29], "Harness")
	require.NotContains(t, view, "connected")
	require.NotContains(t, rows[29], ":999")
	for y := 23; y < 30; y++ {
		require.Equal(t, hitStatus, m.hitTest(5, y), "footer must not map into the diff")
	}
	require.Equal(t, hitNone, m.hitTest(5, 22))
	require.Equal(t, hitDiff, m.hitTest(5, 21))
	m.closeCommand()
	require.Equal(t, originalHeight, m.layout.viewport.Height)
	require.NotContains(t, ansi.Strip(m.View()), "Line 999 is not shown")
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
		model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
		m = model.(Model)
		assert.False(t, m.command.active)
		if annotation {
			assert.Equal(t, ":", m.annot.input.Value())
		} else {
			assert.Equal(t, ":", m.search.input.Value())
		}
	}
}
