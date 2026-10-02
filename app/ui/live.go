package ui

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/umputun/revdiff/app/annotation"
	"github.com/umputun/revdiff/app/diff"
	"github.com/umputun/revdiff/app/keymap"
	"github.com/umputun/revdiff/app/ui/overlay"
)

// FeedbackSender delivers feedback to a bound harness session.
// Send accepts general messages or formatted annotations and returns nil only
// after the harness acknowledges delivery. Calls are serial, with identical
// content retried after a failure.
// Name methods return cached metadata and must not perform IO.
type FeedbackSender interface {
	Send(content string) error
	HarnessName() string
	DisplayName() string
}

// Stager stages or unstages reviewed hunks and entire files.
type Stager interface {
	StageHunk(path string, displayed []diff.DiffLine, cursor int) error
	StageFile(path, oldPath string) error
	UnstageHunk(path string, displayed []diff.DiffLine, cursor int) error
	UnstageFile(path, oldPath string) error
}

type discoveryState int

const (
	discoveryIdle discoveryState = iota
	discoveryBackground
	discoveryForConnect
	discoveryForSend
	discoveryDisabled
	discoveryChoosing
	discoveryChoosingForSend
)

type liveOperation int

const (
	liveIdle liveOperation = iota
	liveSending
	liveStaging
)

type feedbackKind int

const (
	feedbackAnnotations feedbackKind = iota
	feedbackMessage
)

// Discovery may overlap staging. Sending and staging are mutually exclusive;
// a failed send returns to idle with its pending snapshot retained for retry.
type liveState struct {
	sender       FeedbackSender
	discover     func() ([]FeedbackSender, error)
	harnesses    map[string]func() ([]FeedbackSender, error)
	discovery    discoveryState
	discoverySeq uint64
	candidates   []FeedbackSender
	stager       Stager
	operation    liveOperation
	err          error
	pending      []annotation.Annotation
	content      string
	kind         feedbackKind
	stageAnchor  *stageAnchor
}

// stageAnchor follows the unchanged side: working-tree lines when staging,
// HEAD lines when unstaging, rather than the changing diff index/type.
// seq binds it first to the file-list reload, then to its selected file request.
type stageAnchor struct {
	file   string
	seq    uint64
	line   int
	row    int
	staged bool
}

type liveTickMsg struct{ seq uint64 }
type harnessDiscoveryTickMsg struct{}
type harnessesDiscoveredMsg struct {
	sessions []FeedbackSender
	err      error
	seq      uint64
}
type feedbackSentMsg struct{ err error }
type stagedMsg struct {
	action  keymap.Action
	unstage bool
	err     error
}
type liveLoadedMsg struct {
	files filesLoadedMsg
	file  fileLoadedMsg
	seq   uint64
}

// connectHarness performs a user-requested lookup without sending annotations.
// Once bound, preserve the connection and its retry cache rather than switching threads.
func (m Model) connectHarness(name string) (tea.Model, tea.Cmd) {
	if m.live.sender != nil {
		m.output.hint = "Already connected; " + m.feedbackStatusText(m.layout.width)
		return m, nil
	}
	if m.live.discovery != discoveryIdle && m.live.discovery != discoveryDisabled {
		m.output.hint = "Harness discovery in progress; retry after it finishes"
		return m, nil
	}
	connect := m.live.harnesses[name]
	if connect == nil {
		m.output.hint = "Harness unavailable in this review: " + name
		return m, nil
	}
	m.live.discovery = discoveryForConnect
	m.output.hint = "Looking for harness " + name + " in this directory"
	seq := m.live.discoverySeq
	return m, func() tea.Msg {
		sessions, err := connect()
		return harnessesDiscoveredMsg{sessions: sessions, err: err, seq: seq}
	}
}

// disconnectHarness detaches this review without stopping the harness session.
// Keep drafts and unconfirmed feedback so reconnecting never loses user input.
func (m Model) disconnectHarness() (tea.Model, tea.Cmd) {
	if m.live.operation == liveSending {
		m.output.hint = "Wait for feedback delivery to finish before disconnecting"
		return m, nil
	}
	m.live.sender, m.live.err = nil, nil
	m.live.candidates = nil
	m.live.discovery = discoveryDisabled
	m.live.discoverySeq++ // invalidate a lookup that may still be running
	if m.overlay.Kind() == overlay.KindSessions {
		m.overlay.Close()
	}
	m.output.hint = "Harness disconnected; use :harness connect <type> to reconnect"
	return m, nil
}

// Schedule the next lookup only after the previous one completes. While a
// choice is pending, ticks can show the picker after another modal closes.
func (m Model) harnessDiscoveryTick() tea.Cmd {
	choosing := m.live.discovery == discoveryChoosing || m.live.discovery == discoveryChoosingForSend
	if m.live.sender != nil || (m.live.discover == nil && !choosing) || m.live.discovery == discoveryDisabled {
		return nil
	}
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return harnessDiscoveryTickMsg{} })
}

// discoverHarnesses looks up live sessions without requesting feedback delivery.
func (m Model) discoverHarnesses() (tea.Model, tea.Cmd) {
	if m.live.discovery == discoveryChoosing || m.live.discovery == discoveryChoosingForSend {
		m.showHarnessPicker()
		return m, m.harnessDiscoveryTick()
	}
	if m.live.sender != nil || m.live.discover == nil || m.live.discovery != discoveryIdle {
		return m, nil
	}
	m.live.discovery = discoveryBackground
	discover := m.live.discover
	seq := m.live.discoverySeq
	return m, func() tea.Msg {
		sessions, err := discover()
		return harnessesDiscoveredMsg{sessions: sessions, err: err, seq: seq}
	}
}

func (m Model) handleHarnessesDiscovered(msg harnessesDiscoveredMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.live.discoverySeq {
		return m, nil
	}
	m.live.err = msg.err
	requested := m.live.discovery == discoveryForSend || m.live.discovery == discoveryForConnect
	if msg.err != nil || len(msg.sessions) == 0 {
		m.live.discovery = discoveryIdle
		if requested {
			m.output.hint = "Harness not connected; start a session in this directory, then press O"
			if msg.err != nil {
				m.output.hint = msg.err.Error()
			}
		}
		return m, m.harnessDiscoveryTick()
	}
	m.live.candidates = msg.sessions
	if len(msg.sessions) > 1 {
		// These are alternatives. Only the selected session becomes connected.
		if m.live.discovery == discoveryForSend {
			m.live.discovery = discoveryChoosingForSend
		} else {
			m.live.discovery = discoveryChoosing
		}
		m.showHarnessPicker()
		return m, m.harnessDiscoveryTick()
	}
	return m.chooseHarness(0)
}

func (m *Model) showHarnessPicker() {
	if m.liveInteractionActive() {
		return // never replace an annotation draft, command, or another popup
	}
	labels := make([]string, 0, len(m.live.candidates))
	for index, session := range m.live.candidates {
		labels = append(labels, fmt.Sprintf("%d. %s: %s", index+1, session.HarnessName(), session.DisplayName()))
	}
	m.overlay.OpenSessions(labels)
}

func (m Model) chooseHarness(index int) (tea.Model, tea.Cmd) {
	if index < 0 || index >= len(m.live.candidates) {
		return m, nil
	}
	pendingSend := m.live.discovery == discoveryForSend || m.live.discovery == discoveryChoosingForSend
	m.bindHarness(m.live.candidates[index])
	if pendingSend {
		model, cmd := m.sendFeedback()
		return model, tea.Batch(cmd, m.liveTick())
	}
	return m, m.liveTick()
}

func (m *Model) bindHarness(sender FeedbackSender) {
	m.live.sender = sender
	m.live.candidates = nil
	m.live.discovery = discoveryIdle
	m.output.hint = "Harness connected; press O to send feedback"
}

func (m Model) liveTick() tea.Cmd {
	if m.live.sender == nil {
		return nil
	}
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return liveTickMsg{seq: m.live.discoverySeq} })
}

func (m Model) livePaused() bool {
	return !m.filesLoaded || m.file.requestedPath != "" || m.store.Count() > 0 ||
		m.live.operation != liveIdle || m.live.content != "" || m.liveInteractionActive()
}

func (m Model) liveInteractionActive() bool {
	return m.annot.annotating || m.search.active || m.nav.scanKind != treeScanIdle ||
		m.command.active || m.message.active || m.overlay.Active() || m.reload.pending
}

func (m Model) pollLive() (tea.Model, tea.Cmd) {
	if m.live.sender == nil {
		return m, nil
	}
	if m.livePaused() {
		return m, m.liveTick()
	}
	// Capture all tree-derived inputs on the UI goroutine, before starting IO.
	files, file := m.loadFiles(), m.loadFileDiff(m.file.name)
	return m, func() tea.Msg {
		msg := liveLoadedMsg{files: files().(filesLoadedMsg), file: fileLoadedMsg{seq: m.file.loadSeq}, seq: m.live.discoverySeq}
		if m.file.name != "" {
			msg.file = file().(fileLoadedMsg)
		}
		return msg
	}
}

func (m Model) handleLiveLoaded(msg liveLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.live.discoverySeq {
		return m, nil
	}
	tick := m.liveTick()
	// Re-check after IO: a draft, navigation, or manual reload may have started.
	if m.live.sender == nil || m.livePaused() || msg.files.seq != m.filesLoadSeq || msg.file.seq != m.file.loadSeq || msg.file.file != m.file.name {
		return m, tick
	}
	if msg.files.err != nil || msg.file.err != nil {
		m.output.hint = "Live refresh failed; retrying"
		return m, tick
	}
	if slices.Equal(m.review.entries, m.filterOnly(msg.files.entries)) && slices.Equal(m.file.lines, msg.file.lines) {
		return m, tick
	}
	oldName, cursor, offset := m.file.name, m.nav.diffCursor, m.layout.viewport.YOffset()
	var anchor diff.DiffLine
	if cursor >= 0 && cursor < len(m.file.lines) {
		anchor = m.file.lines[cursor]
	}
	model, load := m.handleFilesLoaded(msg.files)
	m = model.(Model)
	if m.tree.SelectedFile() != oldName || oldName == "" || (m.cfg.workingTree && m.selectedTreeStaged() != msg.file.staged) {
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
	if m.live.sender == nil {
		switch m.live.discovery {
		case discoveryDisabled:
			m.output.hint = "Harness disconnected; use :harness connect <type> before sending"
			return m, nil
		case discoveryChoosing, discoveryChoosingForSend:
			m.live.discovery = discoveryChoosingForSend
			return m, nil
		default:
			model, cmd := m.discoverHarnesses()
			m = model.(Model)
			if m.live.discovery != discoveryIdle {
				m.live.discovery = discoveryForSend
				m.output.hint = "Looking for a harness in this directory"
			}
			return m, cmd
		}
	}
	if m.live.content == "" {
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
		m.live.kind = feedbackAnnotations
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
		if m.message.active {
			m.message.err = msg.err.Error() + " · Enter retries the same message"
		}
		return m, nil
	}
	if m.live.kind == feedbackMessage {
		m.live.content = ""
		m.live.kind = feedbackAnnotations
		m.message.draft = ""
		if m.message.active {
			m.message.input.SetValue("")
			m.closeHarnessMessage()
		}
		m.output.hint = "Message sent"
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

func (m Model) stagedContext() bool {
	return m.cfg.workingTree && ((m.layout.focus == paneTree && m.selectedTreeStaged()) || (m.layout.focus == paneDiff && m.file.staged))
}

func (m Model) handleStage(action keymap.Action) (tea.Model, tea.Cmd) {
	if m.live.stager == nil {
		m.output.hint = "Staging requires a Git working-tree review"
		return m, nil
	}
	unstage := m.stagedContext()
	operation, progress := "staging", "Staging"
	if unstage {
		operation, progress = "unstaging", "Unstaging"
	}
	path := m.file.name
	if action == keymap.ActionStageFile && m.layout.focus == paneTree {
		path = m.tree.SelectedFile()
	}
	oldPath := m.tree.OldPath(path)
	// Index edits change only these paths. The reload preserves the store and
	// retry snapshot, so annotations on unrelated files remain safe to send.
	affects := func(a annotation.Annotation) bool { return a.File == path || (oldPath != "" && a.File == oldPath) }
	notes := len(m.store.Get(path))
	if oldPath != "" && oldPath != path {
		notes += len(m.store.Get(oldPath))
	}
	switch {
	case action == keymap.ActionStageHunk && m.layout.focus != paneDiff:
		m.output.hint = "Focus the diff pane before " + operation
	case !m.filesLoaded || m.file.requestedPath != "":
		m.output.hint = "Wait for the diff to finish loading before " + operation
	case m.live.operation == liveSending:
		m.output.hint = "Wait for feedback to finish sending before " + operation
	case slices.ContainsFunc(m.live.pending, affects):
		m.output.hint = "Retry the unconfirmed feedback for this file before " + operation
	case notes > 0:
		m.output.hint = fmt.Sprintf("Send or remove annotations for this file before %s (%d pending)", operation, notes)
	case m.live.operation != liveIdle || m.liveInteractionActive():
		m.output.hint = "Finish the current interaction before " + operation
	default:
		if action == keymap.ActionStageFile {
			if path == "" || !slices.Contains(m.tree.VisibleFiles(), path) {
				m.output.hint = "Select a file before " + operation
				return m, nil
			}
			m.live.operation = liveStaging
			m.output.hint = progress + " file"
			stager := m.live.stager
			update := stager.StageFile
			if unstage {
				update = stager.UnstageFile
			}
			return m, func() tea.Msg { return stagedMsg{action: action, unstage: unstage, err: update(path, oldPath)} }
		}
		if m.tree.FileStatus(m.file.name) != diff.FileModified {
			m.output.hint = "Hunk " + operation + " supports modified tracked text files only"
			return m, nil
		}
		m.live.operation = liveStaging
		m.output.hint = progress + " hunk"
		stager, path, lines, cursor := m.live.stager, m.file.name, slices.Clone(m.file.lines), m.nav.diffCursor
		update := stager.StageHunk
		if unstage {
			update = stager.UnstageHunk
		}
		return m, func() tea.Msg { return stagedMsg{action: action, unstage: unstage, err: update(path, lines, cursor)} }
	}
	return m, nil
}

func (m Model) captureStageAnchor() *stageAnchor {
	if m.nav.diffCursor < 0 || m.nav.diffCursor >= len(m.file.lines) {
		return nil
	}
	a := &stageAnchor{file: m.file.name, seq: m.file.loadSeq, row: m.cursorViewportY() - m.layout.viewport.YOffset(), staged: m.file.staged}
	// Follow the next surviving line when the cursor has no number on the
	// stable side, falling back to the preceding line at EOF.
	for i := m.nav.diffCursor; i < len(m.file.lines); i++ {
		if line := a.lineNumber(m.file.lines[i]); line > 0 {
			a.line = line
			return a
		}
	}
	for i := m.nav.diffCursor - 1; i >= 0; i-- {
		if line := a.lineNumber(m.file.lines[i]); line > 0 {
			a.line = line
			break
		}
	}
	return a
}

func (a stageAnchor) lineNumber(line diff.DiffLine) int {
	if a.staged {
		return line.OldNum
	}
	return line.NewNum
}

func (m *Model) applyStageAnchor(a *stageAnchor) {
	nearest := -1
	for i, line := range m.file.lines {
		n := a.lineNumber(line)
		if n <= 0 {
			continue
		}
		distance := max(n-a.line, a.line-n)
		if nearest < 0 || distance < max(a.lineNumber(m.file.lines[nearest])-a.line, a.line-a.lineNumber(m.file.lines[nearest])) {
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
