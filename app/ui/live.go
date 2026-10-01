package ui

import (
	"encoding/json"
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/umputun/revdiff/app/annotation"
	"github.com/umputun/revdiff/app/diff"
)

// FeedbackSender acknowledges feedback after appending it to the selected thread.
type FeedbackSender interface {
	Send(content string) error
}

// HunkStager stages the displayed change, rejecting stale content.
type HunkStager interface {
	StageHunk(path string, displayed []diff.DiffLine, cursor int) error
}

type liveState struct {
	sender  FeedbackSender
	stager  HunkStager
	sending bool
	staging bool
	pending []annotation.Annotation
	content string
}

type liveTickMsg struct{}
type feedbackSentMsg struct{ err error }
type hunkStagedMsg struct{ err error }
type liveLoadedMsg struct {
	files filesLoadedMsg
	file  fileLoadedMsg
}

func (m Model) liveTick() tea.Cmd {
	if m.live.sender == nil {
		return nil
	}
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return liveTickMsg{} })
}

func (m Model) livePaused() bool {
	return !m.filesLoaded || m.file.requestedPath != "" || m.annot.annotating || m.store.Count() > 0 ||
		m.live.sending || m.live.staging || len(m.live.pending) > 0 || m.search.active ||
		m.overlay.Active() || m.reload.pending || m.inConfirmDiscard
}

func (m Model) pollLive() (tea.Model, tea.Cmd) {
	if m.livePaused() {
		return m, m.liveTick()
	}
	// Capture all tree-derived inputs on the UI goroutine, before starting IO.
	files, file := m.loadFiles(), m.loadFileDiff(m.file.name)
	return m, func() tea.Msg {
		msg := liveLoadedMsg{files: files().(filesLoadedMsg), file: fileLoadedMsg{seq: m.file.loadSeq}}
		if m.file.name != "" {
			msg.file = file().(fileLoadedMsg)
		}
		return msg
	}
}

func (m Model) handleLiveLoaded(msg liveLoadedMsg) (tea.Model, tea.Cmd) {
	tick := m.liveTick()
	// Re-check after IO: a draft, navigation, or manual reload may have started.
	if m.livePaused() || msg.files.seq != m.filesLoadSeq || msg.file.seq != m.file.loadSeq || msg.file.file != m.file.name {
		return m, tick
	}
	if msg.files.err != nil || msg.file.err != nil {
		m.output.hint = "Live refresh failed; retrying"
		return m, tick
	}
	if slices.Equal(m.review.entries, m.filterOnly(msg.files.entries)) && slices.Equal(m.file.lines, msg.file.lines) {
		return m, tick
	}
	oldName, cursor, offset := m.file.name, m.nav.diffCursor, m.layout.viewport.YOffset
	var anchor diff.DiffLine
	if cursor >= 0 && cursor < len(m.file.lines) {
		anchor = m.file.lines[cursor]
	}
	model, load := m.handleFilesLoaded(msg.files)
	m = model.(Model)
	if m.tree.SelectedFile() != oldName || oldName == "" {
		return m, tea.Batch(load, tick)
	}
	// The snapshot already contains this file. Apply it atomically rather than
	// issuing another load that could arrive after the user begins an annotation.
	msg.file.seq = m.file.loadSeq
	model, cmd := m.handleFileLoaded(msg.file)
	m = model.(Model)
	line := anchor.NewNum
	if anchor.ChangeType == diff.ChangeRemove {
		line = anchor.OldNum
	}
	if idx := m.findDiffLineIndex(line, string(anchor.ChangeType)); idx >= 0 {
		cursor = idx
	}
	m.nav.diffCursor = max(0, min(cursor, len(m.file.lines)-1))
	m.layout.viewport.SetContent(m.renderDiff())
	m.layout.viewport.SetYOffset(offset)
	return m, tea.Batch(cmd, tick)
}

func (m Model) sendFeedback() (tea.Model, tea.Cmd) {
	if m.live.sending || m.live.staging {
		return m, nil
	}
	if len(m.live.pending) == 0 {
		if m.store.Count() == 0 {
			m.output.hint = "No annotations to send"
			return m, nil
		}
		content := m.store.FormatOutput()
		encoded, _ := json.Marshal(content) // strings always marshal successfully
		if len(encoded) > 1024*1024-128 {
			m.output.hint = "Feedback exceeds 1 MiB; send fewer annotations"
			return m, nil
		}
		for _, file := range m.store.Files() {
			m.live.pending = append(m.live.pending, m.store.Get(file)...)
		}
		m.live.content = content
	}
	m.live.sending = true
	m.output.hint = "Sending feedback to Amp"
	sender, content := m.live.sender, m.live.content
	return m, func() tea.Msg { return feedbackSentMsg{err: sender.Send(content)} }
}

func (m Model) handleFeedbackSent(msg feedbackSentMsg) (tea.Model, tea.Cmd) {
	m.live.sending = false
	if msg.err != nil {
		m.output.hint = msg.err.Error()
		return m, nil
	}
	// Do not delete annotations edited or added while the request was in flight.
	for _, sent := range m.live.pending {
		for _, current := range m.store.Get(sent.File) {
			if current == sent {
				m.store.Delete(sent.File, sent.Line, sent.Type)
			}
		}
	}
	m.live.pending, m.live.content = nil, ""
	m.tree.RefreshFilter(m.annotatedFiles())
	m.layout.viewport.SetContent(m.renderDiff())
	m.output.hint = "Feedback sent to Amp"
	return m, nil
}

func (m Model) handleStageHunk() (tea.Model, tea.Cmd) {
	if m.live.stager == nil {
		m.output.hint = "Staging requires an unstaged Git working-tree review"
		return m, nil
	}
	if m.livePaused() || m.layout.focus != paneDiff {
		m.output.hint = "Focus the diff and send or remove annotations before staging"
		return m, nil
	}
	if m.tree.FileStatus(m.file.name) != diff.FileModified {
		m.output.hint = "Hunk staging supports modified tracked text files only"
		return m, nil
	}
	m.live.staging = true
	m.output.hint = "Staging hunk"
	stager, path, lines, cursor := m.live.stager, m.file.name, slices.Clone(m.file.lines), m.nav.diffCursor
	return m, func() tea.Msg { return hunkStagedMsg{err: stager.StageHunk(path, lines, cursor)} }
}
