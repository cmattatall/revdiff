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
	"github.com/umputun/revdiff/app/ui/style"
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
			assert.Contains(t, ansi.Strip(m.statusBarText()), tt.err)
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
		assert.Equal(t, originalHeight-1, m.layout.viewport.Height)
		assert.Equal(t, 1, m.statusBarHeight())
		assert.Contains(t, ansi.Strip(m.View()), ":line number")
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
	for _, width := range []int{12, 30, 100} {
		m.layout.width = width
		assert.LessOrEqual(t, ansi.StringWidth(m.statusBarText()), width)
		assert.False(t, strings.Contains(m.statusBarText(), "\n"))
		rendered := m.resolver.Style(style.StyleKeyStatusBar).Width(width).Render(m.statusBarText())
		assert.Equal(t, 1, lipgloss.Height(rendered), "status padding must not wrap command errors")
	}
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
