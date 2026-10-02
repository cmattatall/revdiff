package ui

import (
	"fmt"
	"log"

	tea "charm.land/bubbletea/v2"
)

// outputState holds transient feedback for the O in-session output flush.
// hint is a status-bar message cleared on the next key press, mirroring
// reloadState.hint.
type outputState struct {
	hint string // transient status-bar message, cleared on next key press
	// saved tracks successful file/hook flushes for the quit guard.
	// Harness delivery removes acknowledged annotations instead.
	saved string
}

type postFlushFinishedMsg struct {
	err         error
	content     string
	successHint string
	failureHint string
}

// handleFlushOutput exports the current annotations through the configured
// output file and/or post-flush command without exiting. The store is never
// mutated, so annotations persist in-session and can be re-flushed. Feedback
// is reported through output.hint.
func (m Model) handleFlushOutput() (tea.Model, tea.Cmd) {
	if m.live.sender != nil {
		return m.sendFeedback()
	}
	if m.live.discover != nil || m.live.discovery != discoveryIdle {
		return m.discoverFeedback(true)
	}
	n := m.store.Count()
	if n == 0 {
		m.output.hint = "No annotations to flush"
		return m, nil
	}
	if m.cfg.outputPath == "" && m.postFlushHook == nil {
		m.output.hint = "Output flush requires -o/--output or --post-flush-command"
		return m, nil
	}
	noun := "annotations"
	if n == 1 {
		noun = "annotation"
	}

	var content, writtenHint string
	if m.cfg.outputPath != "" {
		var err error
		content, err = m.store.WriteFile(m.cfg.outputPath)
		if err != nil {
			log.Printf("[WARN] flush annotations to output: %v", err)
			m.output.hint = "Flush failed"
			return m, nil
		}
		writtenHint = fmt.Sprintf("Wrote %d %s to output file", n, noun)
	} else {
		content = m.store.FormatOutput()
	}

	if m.postFlushHook == nil {
		m.output.saved = content
		m.output.hint = writtenHint
		return m, nil
	}

	runningHint := fmt.Sprintf("Running post-flush command with %d %s", n, noun)
	successHint := fmt.Sprintf("Ran post-flush command with %d %s", n, noun)
	failureHint := "Post-flush command failed"
	if writtenHint != "" {
		runningHint = writtenHint + "; running post-flush command"
		successHint = writtenHint + " and ran post-flush command"
		failureHint = writtenHint + "; post-flush command failed"
	}
	cmd := m.postFlushHook.Prepare(content)
	m.output.hint = runningHint
	return m, tea.ExecProcess(cmd, func(runErr error) tea.Msg {
		return postFlushFinishedMsg{
			err:         runErr,
			content:     content,
			successHint: successHint,
			failureHint: failureHint,
		}
	})
}

func (m Model) handlePostFlushFinished(msg postFlushFinishedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		log.Printf("[WARN] post-flush command failed: %v", msg.err)
		m.output.hint = msg.failureHint
		return m, nil
	}
	m.output.saved = msg.content
	m.output.hint = msg.successHint
	return m, nil
}

func (m Model) quitError(force bool) string {
	unsent := m.store.Count() > 0 && m.store.FormatOutput() != m.output.saved
	hint := ""
	if m.live.operation != liveIdle || m.live.discovery == discoveryForSend {
		hint = "Wait for the current review operation to finish"
	} else if !force && (unsent || m.live.content != "" || m.message.draft != "") {
		hint = "Unsent annotations: :w to send, :q! to discard and quit"
		if m.message.draft != "" || m.live.kind == feedbackMessage {
			hint = "Unsent message: :hs to send, :q! to discard and quit"
		}
	}
	return hint
}

func (m Model) quitReview(force bool) (tea.Model, tea.Cmd) {
	if hint := m.quitError(force); hint != "" {
		var cmd tea.Cmd
		if !m.command.active {
			cmd = m.startCommand()
			m.command.input.SetValue("q")
		}
		m.command.err = hint
		return m, cmd
	}
	if force {
		m.store.Clear()
	}
	m.closeCommand()
	return m, tea.Quit
}
