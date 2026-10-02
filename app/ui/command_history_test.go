package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestCommandHistoryRecordsExecution(t *testing.T) {
	m := testModel(nil, nil)
	for _, command := range []string{"fd", "set wrap", "fd", "not a command", ""} {
		m.startCommand()
		m.command.input.SetValue(command)
		model, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = model.(Model)
	}
	require.Equal(t, []string{"set wrap", "focus diff"}, m.command.history)
	m.startCommand()
	m.command.input.SetValue("unfinished")
	m.closeCommand()
	m.startCommand()
	require.Equal(t, []string{"set wrap", "focus diff"}, m.command.history, "canceling must not record the draft")
}

func TestCommandHistoryBoundedAndFuzzy(t *testing.T) {
	m := testModel(nil, nil)
	m.startCommand()
	for i := range 51 {
		m.command.remember(fmt.Sprint(i))
	}
	require.Len(t, m.command.history, 50)
	require.Equal(t, "1", m.command.history[0])
	require.Equal(t, "50", m.command.history[49])
	m.command.history = []string{"view word diff", "view wrap", "view wide", "α β γ"}
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"VWD", []string{"view wide", "view word diff"}},
		{"dwv", nil},
		{"ww", []string{"view wide", "view wrap", "view word diff"}},
		{"ΑΓ", []string{"α β γ"}},
	} {
		m.command.input.SetValue(tc.query)
		require.Equal(t, tc.want, m.command.historyMatches(), tc.query)
	}
}

func TestCommandHistorySearchRecallsWithoutExecuting(t *testing.T) {
	m := testModel(nil, nil)
	m.command.history = []string{"view word diff", "view wrap", "quit"}
	m.startCommand()
	m.layout.width = 110
	for _, msg := range []tea.KeyMsg{
		{Type: tea.KeyCtrlR},
		{Type: tea.KeyRunes, Runes: []rune("vw")},
		{Type: tea.KeyCtrlR},
	} {
		model, _ := m.Update(msg)
		m = model.(Model)
	}
	require.True(t, m.command.historySearch)
	view := ansi.Strip(m.commandPaneView())
	require.Contains(t, view, "History (2/3)")
	require.Contains(t, view, "view wrap")
	require.Contains(t, view, "> view word diff")
	require.Equal(t, "vw", m.command.input.Value(), "selection must not replace the filter")
	// Editing resets the selected result instead of retaining an out-of-range index.
	for _, msg := range []tea.KeyMsg{{Type: tea.KeyCtrlU}, {Type: tea.KeyRunes, Runes: []rune("q")}} {
		model, _ := m.Update(msg)
		m = model.(Model)
	}
	require.Zero(t, m.command.selected)
	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(Model)
	require.Nil(t, cmd, "recall must not execute quit")
	require.True(t, m.command.active)
	require.False(t, m.command.historySearch)
	require.Equal(t, "quit", m.command.input.Value())
	model, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	require.IsType(t, tea.QuitMsg{}, cmd())
	require.False(t, model.(Model).command.active)
}

func TestCommandHistorySearchCancelAndNoMatches(t *testing.T) {
	m := testModel(nil, nil)
	m.command.history = []string{"quit"}
	m.startCommand()
	m.command.input.SetValue("draft")
	for _, key := range []tea.KeyType{tea.KeyCtrlR, tea.KeyCtrlU} {
		model, _ := m.Update(tea.KeyMsg{Type: key})
		m = model.(Model)
	}
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("no match")})
	m = model.(Model)
	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(Model)
	require.Nil(t, cmd)
	require.True(t, m.command.historySearch)
	require.Contains(t, ansi.Strip(m.commandPaneView()), "No matching history")
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = model.(Model)
	require.True(t, m.command.active)
	require.Equal(t, "draft", m.command.input.Value(), "search cancellation restores the input")
}

func TestCommandHistoryListScrollAndResize(t *testing.T) {
	m := testModel(nil, nil)
	for i := range 12 {
		m.command.remember(fmt.Sprintf("command %02d", i))
	}
	m.startCommand()
	model, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 32})
	m = model.(Model)
	normalHeight := m.layout.viewport.Height
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = model.(Model)
	require.Equal(t, normalHeight-8, m.layout.viewport.Height)
	view := ansi.Strip(m.commandPaneView())
	require.Contains(t, view, "> command 11")
	require.Contains(t, view, "command 04")
	require.NotContains(t, view, "command 03", "only the visible window should render")
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = model.(Model)
	require.Equal(t, 8, m.command.selected)
	require.Contains(t, ansi.Strip(m.commandPaneView()), "> command 03")
	for _, size := range []tea.WindowSizeMsg{{Width: 45, Height: 20}, {Width: 16, Height: 14}} {
		model, _ = m.Update(size)
		m = model.(Model)
		view = ansi.Strip(m.commandPaneView())
		require.Contains(t, view, "> command 03", "resizing keeps the selection visible")
		require.Len(t, strings.Split(view, "\n"), m.commandPaneHeight())
		for _, line := range strings.Split(view, "\n") {
			require.LessOrEqual(t, ansi.StringWidth(line), size.Width)
		}
	}
	for _, key := range []tea.KeyType{tea.KeyEnter, tea.KeyTab, tea.KeyEsc} {
		model, _ = m.Update(tea.KeyMsg{Type: key})
		m = model.(Model)
		require.False(t, m.command.historySearch)
		require.Equal(t, m.paneHeight()-1, m.layout.viewport.Height)
		model, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
		m = model.(Model)
	}
}
