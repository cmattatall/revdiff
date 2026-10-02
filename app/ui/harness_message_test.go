package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/umputun/revdiff/app/annotation"
)

func TestHarnessMessageSendPreservesAnnotations(t *testing.T) {
	for _, command := range []string{"harness send", "hs"} {
		for _, focus := range []pane{paneTree, paneDiff} {
			m := testModel(nil, nil)
			m.layout.focus = focus
			sender := &feedbackStub{}
			m.live.sender = sender
			note := annotation.Annotation{File: "other.go", Line: 9, Comment: "keep this annotation"}
			m.store.Add(note)
			m.startCommand()
			m.command.input.SetValue(command)
			model, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			m = model.(Model)
			require.True(t, m.message.active)
			require.False(t, m.command.active)
			require.Empty(t, sender.content)
			const message = "Please review the overall API, not a specific file."
			model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(message)})
			m = model.(Model)
			model, send := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			m = model.(Model)
			require.NotNil(t, send)
			require.Empty(t, sender.content, "sending must be asynchronous")
			_, duplicate := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			require.Nil(t, duplicate)
			model, _ = m.Update(send())
			m = model.(Model)
			require.Equal(t, []string{message}, sender.content)
			require.Equal(t, []annotation.Annotation{note}, m.store.Get("other.go"))
			require.False(t, m.message.active)
			require.Equal(t, "Message sent", m.output.hint)
			require.Equal(t, focus, m.layout.focus)
			require.Equal(t, []string{"harness send"}, m.command.history, "history records the command, never message bodies")
			model, _ = m.openHarnessMessage()
			m = model.(Model)
			require.Empty(t, m.message.input.Value())
		}
	}
}

func TestHarnessMessageDraftAndResize(t *testing.T) {
	m := testModel(nil, nil)
	m.cfg.noStatusBar = true
	model, _ := m.openHarnessMessage()
	m = model.(Model)
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":! This is message text, not a command")})
	m = model.(Model)
	for _, width := range []int{25, 100} {
		model, _ = m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		m = model.(Model)
		view := ansi.Strip(m.commandPaneView())
		require.Equal(t, 4, lipgloss.Height(view))
		for _, line := range strings.Split(view, "\n") {
			require.LessOrEqual(t, ansi.StringWidth(line), width)
		}
		require.Contains(t, view, "Message:")
	}
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = model.(Model)
	require.False(t, m.message.active)
	m.startCommand()
	m.closeCommand()
	model, _ = m.openHarnessMessage()
	m = model.(Model)
	require.Equal(t, ":! This is message text, not a command", m.message.input.Value())
	require.False(t, m.command.historySearch)
}

func TestHarnessMessageRetryKeepsSnapshotAndAnnotations(t *testing.T) {
	m := testModel(nil, nil)
	sender := &feedbackStub{err: errors.New("delivery unconfirmed")}
	m.live.sender = sender
	m.store.Add(annotation.Annotation{File: "a.go", Line: 2, Comment: "keep"})
	model, _ := m.openHarnessMessage()
	m = model.(Model)
	m.message.input.SetValue("Original message")
	model, send := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(Model)
	model, _ = m.Update(send())
	m = model.(Model)
	require.Contains(t, m.message.err, "Enter retries")
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("must not change pending text")})
	m = model.(Model)
	require.Equal(t, "Original message", m.message.input.Value())
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = model.(Model)
	sender.err = nil
	model, retry := m.handleFlushOutput()
	m = model.(Model)
	model, _ = m.Update(retry())
	m = model.(Model)
	require.Equal(t, []string{"Original message", "Original message"}, sender.content)
	require.Equal(t, 1, m.store.Count())
	require.Empty(t, m.message.draft)
	model, annotations := m.handleFlushOutput()
	m = model.(Model)
	model, _ = m.Update(annotations())
	m = model.(Model)
	require.Contains(t, sender.content[2], "a.go")
	require.Contains(t, sender.content[2], "keep")
	require.Zero(t, m.store.Count())
}

func TestHarnessMessageGuards(t *testing.T) {
	m := testModel(nil, nil)
	model, _ := m.openHarnessMessage()
	m = model.(Model)
	m.message.input.SetValue("Keep this draft")
	model, send := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(Model)
	require.Nil(t, send)
	require.Contains(t, m.message.err, "No harness connected")
	require.Equal(t, "Keep this draft", m.message.input.Value())
	m.live.sender = &feedbackStub{}
	m.message.input.SetValue("  ")
	model, send = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(Model)
	require.Nil(t, send)
	require.Contains(t, m.message.err, "Enter a message")
	m.closeHarnessMessage()
	m.store.Add(annotation.Annotation{File: "a.go", Line: 2, Comment: "annotation snapshot"})
	model, _ = m.sendFeedback()
	m = model.(Model)
	model, cmd := m.openHarnessMessage()
	m = model.(Model)
	require.Nil(t, cmd)
	require.False(t, m.message.active)
	model, _ = m.handleFeedbackSent(feedbackSentMsg{err: errors.New("unconfirmed")})
	m = model.(Model)
	model, cmd = m.openHarnessMessage()
	m = model.(Model)
	require.Nil(t, cmd)
	require.Contains(t, m.output.hint, "Retry the unconfirmed annotations")
	require.Equal(t, feedbackAnnotations, m.live.kind)
}
