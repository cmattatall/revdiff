package ui

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/umputun/revdiff/app/annotation"
	"github.com/umputun/revdiff/app/diff"
	"github.com/umputun/revdiff/app/keymap"
)

// FeedbackSender delivers feedback to a bound harness session.
// Name methods return cached metadata and must not perform IO.
type FeedbackSender interface {
	Send(content string) error
	HarnessName() string
	DisplayName() string
}

// Stager stages reviewed hunks or entire current working-tree files.
type Stager interface {
	StageHunk(path string, displayed []diff.DiffLine, cursor int) error
	StageFile(path, oldPath string) error
}

type discoveryState int

const (
	discoveryIdle discoveryState = iota
	discoveryBackground
	discoveryForSend
)

type liveOperation int

const (
	liveIdle liveOperation = iota
	liveSending
	liveStaging
)

// Discovery may overlap staging. Sending and staging are mutually exclusive;
// a failed send returns to idle with its pending snapshot retained for retry.
type liveState struct {
	sender      FeedbackSender
	discover    func() (FeedbackSender, error)
	discovery   discoveryState
	stager      Stager
	operation   liveOperation
	err         error
	pending     []annotation.Annotation
	content     string
	stageAnchor *stageAnchor
}

// stageAnchor follows the working-tree line, not its changing diff index/type.
// seq binds it first to the file-list reload, then to its selected file request.
type stageAnchor struct {
	file string
	seq  uint64
	line int
	row  int
}

type liveTickMsg struct{}
type feedbackTickMsg struct{}
type feedbackDiscoveredMsg struct {
	sender FeedbackSender
	err    error
}
type feedbackSentMsg struct{ err error }
type stagedMsg struct {
	action keymap.Action
	err    error
}
type liveLoadedMsg struct {
	files filesLoadedMsg
	file  fileLoadedMsg
}

// Discovery has its own timer so an O-triggered lookup cannot multiply the
// refresh loop. It stops once a sender is bound; pending retries never switch threads.
func (m Model) feedbackTick() tea.Cmd {
	if m.live.sender != nil || m.live.discover == nil {
		return nil
	}
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return feedbackTickMsg{} })
}

func (m Model) discoverFeedback(send bool) (tea.Model, tea.Cmd) {
	if m.live.sender != nil || m.live.discover == nil {
		return m, nil
	}
	previous := m.live.discovery
	if send {
		m.live.discovery = discoveryForSend
		m.output.hint = "Looking for a harness in this directory"
	} else if previous == discoveryIdle {
		m.live.discovery = discoveryBackground
	}
	if previous != discoveryIdle {
		return m, nil
	}
	discover := m.live.discover
	return m, func() tea.Msg {
		sender, err := discover()
		return feedbackDiscoveredMsg{sender: sender, err: err}
	}
}

func (m Model) handleFeedbackDiscovered(msg feedbackDiscoveredMsg) (tea.Model, tea.Cmd) {
	m.live.err = msg.err
	send := m.live.discovery == discoveryForSend
	m.live.discovery = discoveryIdle
	if msg.err != nil || msg.sender == nil {
		if send {
			m.output.hint = "Harness not connected; start a session in this directory, then press O"
			if msg.err != nil {
				m.output.hint = msg.err.Error()
			}
		}
		return m, nil
	}
	m.live.sender = msg.sender
	if send {
		model, cmd := m.sendFeedback()
		return model, tea.Batch(cmd, m.liveTick())
	}
	m.output.hint = "Harness connected; press O to send feedback"
	return m, m.liveTick()
}

func (m Model) liveTick() tea.Cmd {
	if m.live.sender == nil {
		return nil
	}
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return liveTickMsg{} })
}

func (m Model) livePaused() bool {
	return !m.filesLoaded || m.file.requestedPath != "" || m.annot.annotating || m.store.Count() > 0 ||
		m.live.operation != liveIdle || len(m.live.pending) > 0 || m.search.active ||
		m.search.scanPhase == searchScanRunning || m.command.active || m.overlay.Active() || m.reload.pending
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
	if m.live.operation != liveIdle {
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
	m.live.operation = liveSending
	m.output.hint = "Sending feedback"
	sender, content := m.live.sender, m.live.content
	return m, func() tea.Msg { return feedbackSentMsg{err: sender.Send(content)} }
}

func (m Model) handleFeedbackSent(msg feedbackSentMsg) (tea.Model, tea.Cmd) {
	m.live.operation = liveIdle
	m.live.err = msg.err
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
	m.output.hint = "Feedback sent"
	return m, nil
}

func (m Model) handleStage(action keymap.Action) (tea.Model, tea.Cmd) {
	if m.live.stager == nil {
		m.output.hint = "Staging requires an unstaged Git working-tree review"
		return m, nil
	}
	// Explain the actual blocker instead of treating every refresh pause as an
	// annotation problem. Staging reloads the diff, so pending notes stay guarded.
	switch {
	case action == keymap.ActionStageHunk && m.layout.focus != paneDiff:
		m.output.hint = "Focus the diff pane before staging"
	case !m.filesLoaded || m.file.requestedPath != "":
		m.output.hint = "Wait for the diff to finish loading before staging"
	case m.live.operation == liveSending:
		m.output.hint = "Wait for feedback to finish sending before staging"
	case len(m.live.pending) > 0:
		m.output.hint = "Retry the unconfirmed feedback before staging"
	case m.store.Count() > 0:
		m.output.hint = fmt.Sprintf("Send or remove annotations before staging (%d pending across all files)", m.store.Count())
	case m.livePaused():
		m.output.hint = "Finish the current interaction before staging"
	default:
		if action == keymap.ActionStageFile {
			path := m.file.name
			if m.layout.focus == paneTree {
				path = m.tree.SelectedFile()
			}
			if path == "" || !slices.Contains(m.tree.VisibleFiles(), path) {
				m.output.hint = "Select a file before staging"
				return m, nil
			}
			m.live.operation = liveStaging
			m.output.hint = "Staging file"
			stager, oldPath := m.live.stager, m.tree.OldPath(path)
			return m, func() tea.Msg { return stagedMsg{action: action, err: stager.StageFile(path, oldPath)} }
		}
		if m.tree.FileStatus(m.file.name) != diff.FileModified {
			m.output.hint = "Hunk staging supports modified tracked text files only"
			return m, nil
		}
		m.live.operation = liveStaging
		m.output.hint = "Staging hunk"
		stager, path, lines, cursor := m.live.stager, m.file.name, slices.Clone(m.file.lines), m.nav.diffCursor
		return m, func() tea.Msg { return stagedMsg{action: action, err: stager.StageHunk(path, lines, cursor)} }
	}
	return m, nil
}

func (m Model) captureStageAnchor() *stageAnchor {
	if m.nav.diffCursor < 0 || m.nav.diffCursor >= len(m.file.lines) {
		return nil
	}
	a := &stageAnchor{file: m.file.name, seq: m.file.loadSeq, row: m.cursorViewportY() - m.layout.viewport.YOffset}
	// A removed line has no working-tree number. Follow its replacement or
	// next surviving line, falling back to the preceding line at EOF.
	for i := m.nav.diffCursor; i < len(m.file.lines); i++ {
		if line := m.file.lines[i].NewNum; line > 0 {
			a.line = line
			return a
		}
	}
	for i := m.nav.diffCursor - 1; i >= 0; i-- {
		if line := m.file.lines[i].NewNum; line > 0 {
			a.line = line
			break
		}
	}
	return a
}

func (m *Model) applyStageAnchor(a *stageAnchor) {
	nearest := -1
	for i, line := range m.file.lines {
		if line.NewNum <= 0 {
			continue
		}
		distance := max(line.NewNum-a.line, a.line-line.NewNum)
		if nearest < 0 || distance < max(m.file.lines[nearest].NewNum-a.line, a.line-m.file.lines[nearest].NewNum) {
			nearest = i
		}
	}
	if nearest >= 0 {
		m.nav.diffCursor = nearest
		m.adjustCursorIfHidden()
	}
	m.layout.viewport.SetContent(m.renderDiff())
	m.layout.viewport.SetYOffset(max(0, m.cursorViewportY()-a.row))
}
