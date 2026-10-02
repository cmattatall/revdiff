package ui

import (
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// ShellRunner prepares a shell command for the terminal handoff.
type ShellRunner interface {
	Prepare(command string) *exec.Cmd
}

type shellFinishedMsg struct{ err error }

func (m *Model) runShellCommand(value string) (tea.Model, tea.Cmd) {
	command := strings.TrimSpace(strings.TrimPrefix(value, "!"))
	if value == "!!" {
		command = m.command.lastShell
		if command == "" {
			m.command.err = "No previous shell command"
			return *m, nil
		}
	}
	if command == "" {
		m.command.err = "Enter a shell command after !"
		return *m, nil
	}
	if err := m.shellError(); err != "" {
		m.command.err = err
		return *m, nil
	}
	m.command.remember("! " + command)
	return m.executeShellCommand(command)
}

func (m *Model) shellError() string {
	if m.shell == nil {
		return "Shell commands are unavailable in this session"
	}
	if m.live.operation != liveIdle {
		return "Wait for the current review operation to finish"
	}
	return ""
}

func (m *Model) executeShellCommand(command string) (tea.Model, tea.Cmd) {
	cmd := m.shell.Prepare(command)
	m.command.lastShell = command
	m.closeCommand()
	return *m, tea.ExecProcess(cmd, func(err error) tea.Msg { return shellFinishedMsg{err: err} })
}

func (m Model) handleShellFinished(msg shellFinishedMsg) (tea.Model, tea.Cmd) {
	m.keys.hint = "Shell command finished"
	if msg.err != nil {
		m.keys.hint = "Shell command failed: " + msg.err.Error()
	}
	cmd := m.loadRepositoryHead()
	return m, cmd
}
