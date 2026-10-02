package ui

import (
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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
	if m.shell == nil {
		m.command.err = "Shell commands are unavailable in this session"
		return *m, nil
	}
	if m.live.operation != liveIdle {
		m.command.err = "Wait for the current review operation to finish"
		return *m, nil
	}
	cmd := m.shell.Prepare(command)
	m.command.lastShell = command
	m.command.remember("! " + command)
	m.closeCommand()
	return *m, tea.ExecProcess(cmd, func(err error) tea.Msg { return shellFinishedMsg{err: err} })
}

func (m Model) handleShellFinished(msg shellFinishedMsg) (tea.Model, tea.Cmd) {
	m.keys.hint = "Shell command finished"
	if msg.err != nil {
		m.keys.hint = "Shell command failed: " + msg.err.Error()
	}
	if m.cfg.mouseTracking {
		return m, tea.EnableMouseCellMotion
	}
	return m, nil
}
