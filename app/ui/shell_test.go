package ui

import (
	"errors"
	"os/exec"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/umputun/revdiff/app/annotation"
)

type shellStub struct{ command string }

func (s *shellStub) Prepare(command string) *exec.Cmd {
	s.command = command
	return &exec.Cmd{}
}

func TestShellCommand_Delegation(t *testing.T) {
	var entry paletteCommand = shellCommand{
		commandEntry: commandEntry{name: "repo status", aliases: []string{"rs"}},
		prefix:       "git status --short",
	}
	for _, tc := range []struct {
		input, want string
	}{
		{"repo status", "git status --short"},
		{`repo status -- 'Mixed Case.txt'`, `git status --short -- 'Mixed Case.txt'`},
		{"rs\t--branch", "git status --short\t--branch"},
		{"REPO STATUS  --branch", "git status --short  --branch"},
		{"repo statuses", ""},
		{"rs-extra", ""},
		{"repo", ""},
	} {
		t.Run(tc.input, func(t *testing.T) {
			require.Equal(t, tc.want != "", entry.matchesInput(tc.input))
			if tc.want == "" {
				return
			}
			m := testModel(nil, nil)
			runner := &shellStub{}
			m.shell = runner
			m.startCommand()
			model, cmd := entry.execute(&m, tc.input)
			require.NotNil(t, cmd)
			require.False(t, model.(Model).command.active)
			require.Equal(t, tc.want, runner.command)
		})
	}
	require.Equal(t, ":repo status <args> (:rs)", entry.helpName())
}

func TestModel_ShellCommandPreservesText(t *testing.T) {
	for _, focus := range []pane{paneTree, paneDiff} {
		m := testModel([]string{"a.go"}, nil)
		m.layout.focus = focus
		runner := &shellStub{}
		m.shell = runner
		m.startCommand()
		m.command.input.SetValue(`! printf '%s' 'Mixed Case; $HOME' | cat`)
		require.Empty(t, m.commandMatches())
		model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = model.(Model)
		require.NotNil(t, cmd)
		require.False(t, m.command.active)
		require.Equal(t, `printf '%s' 'Mixed Case; $HOME' | cat`, runner.command)
		require.Equal(t, []string{`! printf '%s' 'Mixed Case; $HOME' | cat`}, m.command.history)
		require.Equal(t, focus, m.layout.focus)
	}
}

func TestModel_ShellCommandGuards(t *testing.T) {
	for _, tc := range []struct {
		command string
		runner  bool
		busy    bool
		want    string
	}{{"!", true, false, "Enter a shell command"}, {"!!", true, false, "No previous shell command"}, {"! pwd", false, false, "unavailable"}, {"! pwd", true, true, "Wait"}} {
		m := testModel(nil, nil)
		runner := &shellStub{}
		if tc.runner {
			m.shell = runner
		}
		if tc.busy {
			m.live.operation = liveSending
		}
		m.startCommand()
		m.command.input.SetValue(tc.command)
		model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = model.(Model)
		require.Nil(t, cmd)
		require.True(t, m.command.active)
		require.Contains(t, m.command.err, tc.want)
		require.Empty(t, runner.command)
	}
}

func TestModel_GitCommand(t *testing.T) {
	for _, focus := range []pane{paneTree, paneDiff} {
		m := testModel([]string{"a.go"}, nil)
		m.layout.focus = focus
		runner := &shellStub{}
		m.shell = runner
		for _, input := range []string{`git log --format='%s' --grep='Mixed Case'`, "!!"} {
			m.startCommand()
			m.command.input.SetValue(input)
			model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			m = model.(Model)
			require.NotNil(t, cmd)
			require.False(t, m.command.active)
			require.Equal(t, `git log --format='%s' --grep='Mixed Case'`, runner.command)
			require.Equal(t, focus, m.layout.focus)
			runner.command = ""
		}
	}
}

func TestModel_ShellBackedCommandCompletion(t *testing.T) {
	m := testModel(nil, nil)
	runner := &shellStub{}
	m.shell = runner
	m.startCommand()
	m.command.input.SetValue("gi")
	matches := m.commandMatches()
	require.NotEmpty(t, matches)
	require.Equal(t, "git", matches[0].name)
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = model.(Model)
	require.Equal(t, "git", m.command.input.Value())
	m.command.input.SetValue("git  log  --grep='Mixed Case' | cat")
	require.Empty(t, m.commandMatches())
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = model.(Model)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	require.Equal(t, "git  log  --grep='Mixed Case' | cat", runner.command)
}

func TestModel_RepeatShellCommand(t *testing.T) {
	m := testModel([]string{"a.go"}, nil)
	runner := &shellStub{}
	m.shell = runner
	for _, input := range []string{`! printf '%s' 'Mixed Case; $HOME' | cat`, "set number", "!!", "!!"} {
		m.startCommand()
		m.command.input.SetValue(input)
		model, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = model.(Model)
		require.Empty(t, m.command.err)
		require.False(t, m.command.active)
		if input == "set number" {
			require.Empty(t, runner.command)
		} else {
			require.Equal(t, `printf '%s' 'Mixed Case; $HOME' | cat`, runner.command)
		}
		runner.command = ""
	}
}

func TestModel_ShellFinishedPreservesAnnotations(t *testing.T) {
	m := testModel([]string{"a.go"}, nil)
	m.store.Add(annotation.Annotation{File: "a.go", Line: 7, Comment: "keep this"})
	m.cfg.mouseTracking = true
	model, cmd := m.Update(shellFinishedMsg{err: errors.New("exit status 7")})
	m = model.(Model)
	require.Contains(t, m.keys.hint, "exit status 7")
	require.Equal(t, 1, m.store.Count())
	require.NotNil(t, cmd, "restore mouse tracking after terminal handoff")
}
