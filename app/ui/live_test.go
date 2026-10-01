package ui

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/umputun/revdiff/app/annotation"
	"github.com/umputun/revdiff/app/diff"
	"github.com/umputun/revdiff/app/keymap"
)

type feedbackStub struct {
	content []string
	err     error
	harness string
	display string
}

func (s *feedbackStub) HarnessName() string { return s.harness }
func (s *feedbackStub) DisplayName() string { return s.display }

func (s *feedbackStub) Send(content string) error {
	s.content = append(s.content, content)
	return s.err
}

type stagerStub struct {
	hunk func(string, []diff.DiffLine, int) error
	file func(string, string) error
}

func (s stagerStub) StageHunk(path string, lines []diff.DiffLine, cursor int) error {
	return s.hunk(path, lines, cursor)
}

func (s stagerStub) StageFile(path, oldPath string) error {
	return s.file(path, oldPath)
}

func TestStageFileShortcutUsesFocusedSelection(t *testing.T) {
	for _, focus := range []pane{paneTree, paneDiff} {
		for _, stageErr := range []error{nil, errors.New("index locked")} {
			m := testModel([]string{"a.go", "b.go"}, nil)
			entries := []diff.FileEntry{{Path: "a.go", Status: diff.FileModified}, {Path: "b.go", OldPath: "old.go", Status: diff.FileRenamed}}
			model, _ := m.handleFilesLoaded(filesLoadedMsg{entries: entries})
			m = model.(Model)
			model, _ = m.handleFileLoaded(fileLoadedMsg{file: "a.go", seq: m.file.loadSeq})
			m = model.(Model)
			m.tree.SelectByPath("b.go")
			m.layout.focus = focus
			want, old := "a.go", ""
			if focus == paneTree {
				want, old = "b.go", "old.go"
			}
			calls := 0
			m.live.stager = stagerStub{file: func(path, oldPath string) error {
				calls++
				require.Equal(t, want, path)
				require.Equal(t, old, oldPath)
				return stageErr
			}}
			model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
			m = model.(Model)
			require.NotNil(t, cmd, "file staging does not need a changed line under the cursor")
			require.Zero(t, calls, "Git IO must run in the command")
			require.Equal(t, liveStaging, m.live.operation)
			_, duplicate := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
			require.Nil(t, duplicate)
			model, reload := m.Update(cmd())
			m = model.(Model)
			require.Equal(t, 1, calls)
			require.Equal(t, liveIdle, m.live.operation)
			if stageErr != nil {
				require.Nil(t, reload)
				require.Equal(t, "Stage failed: index locked", m.output.hint)
			} else {
				require.NotNil(t, reload)
				require.False(t, m.filesLoaded)
				require.Equal(t, "File staged", m.output.hint)
			}
		}
	}
}

func TestStageFileGuards(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*Model)
		want  string
	}{
		{"unavailable", func(m *Model) { m.live.stager = nil }, "Staging requires"},
		{"loading", func(m *Model) { m.file.requestedPath = "a.go" }, "Wait for the diff"},
		{"sending", func(m *Model) { m.live.operation = liveSending }, "Wait for feedback"},
		{"unconfirmed", func(m *Model) { m.live.pending = []annotation.Annotation{{File: "a.go"}} }, "Retry the unconfirmed"},
		{"annotations", func(m *Model) { m.store.Add(annotation.Annotation{File: "other.go", Line: 1, Comment: "keep"}) }, "Send or remove annotations"},
		{"no selection", func(m *Model) { m.file.name = "" }, "Select a file"},
		{"directory", func(m *Model) { m.file.name = "folder" }, "Select a file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := testModel([]string{"a.go", "folder/b.go"}, nil)
			m.file.name, m.layout.focus = "a.go", paneDiff
			m.live.stager = stagerStub{file: func(string, string) error { t.Fatal("must not stage"); return nil }}
			tc.setup(&m)
			model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
			require.Nil(t, cmd)
			require.Contains(t, model.(Model).output.hint, tc.want)
		})
	}
}

func TestStageHunkReportsActualBlocker(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*Model)
		want  string
	}{
		{"tree focus", func(m *Model) { m.layout.focus = paneTree }, "Focus the diff pane before staging"},
		{"file list loading", func(m *Model) { m.filesLoaded = false }, "Wait for the diff to finish loading before staging"},
		{"diff loading", func(m *Model) { m.file.requestedPath = "other.go" }, "Wait for the diff to finish loading before staging"},
		{"sending", func(m *Model) { m.live.operation = liveSending }, "Wait for feedback to finish sending before staging"},
		{"unconfirmed", func(m *Model) {
			m.live.pending = []annotation.Annotation{{File: "other.go", Line: 7, Comment: "retry me"}}
		}, "Retry the unconfirmed feedback before staging"},
		{"annotations in other files", func(m *Model) {
			m.store.Add(annotation.Annotation{File: "other.go", Line: 7, Comment: "keep me"})
			m.store.Add(annotation.Annotation{File: "third.go", Line: 9, Comment: "keep me too"})
		}, "Send or remove annotations before staging (2 pending across all files)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := testModel([]string{"a.go"}, nil)
			m.layout.focus = paneDiff
			m.live.stager = stagerStub{hunk: func(string, []diff.DiffLine, int) error {
				t.Fatal("blocked shortcut must not stage")
				return nil
			}}
			tc.setup(&m)
			count := m.store.Count()
			model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
			m = model.(Model)
			require.Nil(t, cmd)
			require.NotEqual(t, liveStaging, m.live.operation)
			require.Equal(t, tc.want, m.output.hint)
			if m.filesLoaded {
				require.Contains(t, m.View(), tc.want, "render the actual reason in the status bar")
			} else {
				require.Equal(t, "loading files...", m.View())
			}
			require.Equal(t, count, m.store.Count(), "refusing to stage must preserve annotations")
		})
	}
}

func TestStageShortcutCapturesDisplayedHunk(t *testing.T) {
	for _, stageErr := range []error{nil, errors.New("file changed since display")} {
		m := testModel([]string{"a.go"}, nil)
		loaded, _ := m.handleFilesLoaded(filesLoadedMsg{entries: []diff.FileEntry{{Path: "a.go", Status: diff.FileModified}}})
		m = loaded.(Model)
		lines := []diff.DiffLine{{OldNum: 3, NewNum: 3, Content: "context"}, {NewNum: 4, Content: "new", ChangeType: diff.ChangeAdd}}
		loaded, _ = m.handleFileLoaded(fileLoadedMsg{file: "a.go", lines: lines, seq: m.file.loadSeq})
		m = loaded.(Model)
		m.nav.diffCursor = 1
		m.layout.focus = paneDiff
		calls := 0
		m.live.stager = stagerStub{hunk: func(path string, displayed []diff.DiffLine, cursor int) error {
			calls++
			require.Equal(t, "a.go", path)
			require.Equal(t, "new", displayed[1].Content)
			require.Equal(t, 1, cursor)
			return stageErr
		}}
		sender := &feedbackStub{}
		m.live.discover = func() (FeedbackSender, error) { return sender, nil }
		model, lookup := m.discoverFeedback(false)
		m = model.(Model)
		model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
		m = model.(Model)
		require.NotNil(t, cmd)
		require.Equal(t, liveStaging, m.live.operation)
		require.Equal(t, discoveryBackground, m.live.discovery)
		model, _ = m.Update(lookup())
		m = model.(Model)
		require.Equal(t, discoveryIdle, m.live.discovery)
		require.Equal(t, liveStaging, m.live.operation, "discovery completion must not clear staging")
		require.Same(t, sender, m.live.sender)
		_, blockedSend := m.sendFeedback()
		require.Nil(t, blockedSend, "sending and staging are mutually exclusive")
		require.Zero(t, calls, "staging IO belongs in the command")
		m.file.lines[1].Content = "later"
		model, reload := m.Update(cmd())
		m = model.(Model)
		require.Equal(t, 1, calls)
		require.Equal(t, liveIdle, m.live.operation)
		if stageErr != nil {
			require.Nil(t, reload)
			require.Equal(t, "Stage failed: file changed since display", m.output.hint)
		} else {
			require.NotNil(t, reload)
			require.False(t, m.filesLoaded, "successful staging refreshes the unstaged diff")
			require.Equal(t, "Hunk staged", m.output.hint)
		}
	}
}

func TestStageReloadPreservesPosition(t *testing.T) {
	for _, tc := range []struct {
		name               string
		cursor, wantLine   int
		compact, collapsed bool
	}{
		{"added line becomes context", 58, 57, false, false},
		{"removed line follows replacement", 54, 55, false, false},
		{"compact hunk disappears", 58, 80, true, false},
		{"collapsed diff", 58, 57, false, true},
		{"removed end of file", 102, 100, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var before, after []diff.DiffLine
			for n := 1; n <= 100; n++ {
				line := diff.DiffLine{OldNum: n, NewNum: n, Content: "context", ChangeType: diff.ChangeContext}
				if n == 8 {
					line.ChangeType = diff.ChangeAdd // an unrelated earlier change must not attract the cursor
				}
				if !tc.compact || n <= 10 || n >= 80 {
					after = append(after, line)
				}
				if n == 55 {
					before = append(before, diff.DiffLine{OldNum: 55, Content: "old one", ChangeType: diff.ChangeRemove},
						diff.DiffLine{OldNum: 56, Content: "old two", ChangeType: diff.ChangeRemove})
				}
				if n >= 55 && n <= 57 {
					line.OldNum, line.ChangeType = 0, diff.ChangeAdd
				}
				before = append(before, line)
			}
			before = append(before, diff.DiffLine{OldNum: 101, Content: "removed tail", ChangeType: diff.ChangeRemove})
			m := testModel([]string{"a.go"}, nil)
			entries := []diff.FileEntry{{Path: "a.go", Status: diff.FileModified}}
			model, _ := m.handleFilesLoaded(filesLoadedMsg{entries: entries})
			m = model.(Model)
			model, _ = m.handleFileLoaded(fileLoadedMsg{file: "a.go", seq: m.file.loadSeq, lines: before})
			m = model.(Model)
			m.cfg.startAtChange = true // staging must override this startup/navigation preference
			m.modes.compact, m.modes.collapsed.enabled = tc.compact, tc.collapsed
			m.nav.diffCursor, m.layout.viewport.Height = tc.cursor, 12
			m.layout.viewport.SetContent(m.renderDiff())
			m.layout.viewport.SetYOffset(m.cursorViewportY() - 4)
			row := m.cursorViewportY() - m.layout.viewport.YOffset
			m.live.stager = stagerStub{hunk: func(string, []diff.DiffLine, int) error { return nil }}
			model, stage := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
			m = model.(Model)
			require.NotNil(t, stage)
			model, _ = m.Update(stage())
			m = model.(Model)
			model, _ = m.handleFilesLoaded(filesLoadedMsg{seq: m.filesLoadSeq, entries: entries})
			m = model.(Model)
			model, _ = m.handleFileLoaded(fileLoadedMsg{file: "a.go", seq: m.file.loadSeq, lines: after})
			m = model.(Model)
			require.Equal(t, tc.wantLine, m.file.lines[m.nav.diffCursor].NewNum)
			require.Equal(t, row, m.cursorViewportY()-m.layout.viewport.YOffset, "preserve the cursor's screen row")
			require.Nil(t, m.live.stageAnchor)
		})
	}
}

func TestStageAnchorDoesNotLeakToOtherLoads(t *testing.T) {
	for _, action := range []string{"file disappears", "all files disappear", "navigate before list", "navigate after list", "manual reload", "file error"} {
		t.Run(action, func(t *testing.T) {
			m := testModel([]string{"a.go", "b.go"}, nil)
			lines := []diff.DiffLine{{NewNum: 10, ChangeType: diff.ChangeContext}, {NewNum: 20, ChangeType: diff.ChangeAdd}}
			entries := []diff.FileEntry{{Path: "a.go", Status: diff.FileModified}, {Path: "b.go", Status: diff.FileModified}}
			model, _ := m.handleFilesLoaded(filesLoadedMsg{entries: entries})
			m = model.(Model)
			model, _ = m.handleFileLoaded(fileLoadedMsg{file: "a.go", seq: m.file.loadSeq, lines: lines})
			m = model.(Model)
			m.nav.diffCursor = 1
			model, _ = m.Update(stagedMsg{action: keymap.ActionStageHunk})
			m = model.(Model)
			switch action {
			case "file disappears":
				entries = entries[1:]
			case "all files disappear":
				entries = nil
			case "navigate before list":
				m.requestFileDiff("b.go")
			case "manual reload":
				m.triggerReload()
			}
			model, _ = m.handleFilesLoaded(filesLoadedMsg{seq: m.filesLoadSeq, entries: entries})
			m = model.(Model)
			if action == "all files disappear" {
				require.Empty(t, m.file.name)
				require.Nil(t, m.live.stageAnchor)
				return
			}
			if action == "navigate after list" {
				m.requestFileDiff("b.go")
			}
			msg := fileLoadedMsg{file: m.file.requestedPath, seq: m.file.loadSeq, lines: lines}
			if action == "file error" {
				msg.err = errors.New("read failed")
			}
			model, _ = m.handleFileLoaded(msg)
			m = model.(Model)
			require.Nil(t, m.live.stageAnchor)
			if msg.err == nil {
				require.Zero(t, m.nav.diffCursor, "unrelated loads retain normal positioning")
			}
		})
	}
}

func TestHarnessConnectFailurePreservesAnnotations(t *testing.T) {
	for _, lookupErr := range []error{nil, errors.New("multiple Amp sessions")} {
		m := testNewModel(t, plainRenderer(), annotation.NewStore(), noopHighlighter(), ModelConfig{
			Harnesses: map[string]func() (FeedbackSender, error){"amp": func() (FeedbackSender, error) {
				return nil, lookupErr
			}},
		})
		m.store.Add(annotation.Annotation{File: "a.go", Line: 4, Comment: "preserve"})
		model, cmd := m.connectHarness("amp")
		m = model.(Model)
		require.NotNil(t, cmd)
		_, duplicate := m.connectHarness("amp")
		require.Nil(t, duplicate)
		model, _ = m.Update(cmd())
		m = model.(Model)
		require.Nil(t, m.live.sender)
		require.Equal(t, discoveryIdle, m.live.discovery)
		require.Equal(t, 1, m.store.Count())
		if lookupErr == nil {
			require.Contains(t, m.output.hint, "Harness not connected")
		} else {
			require.Equal(t, lookupErr.Error(), m.output.hint)
		}
	}
}

func TestHarnessSendJoinsManualConnect(t *testing.T) {
	sender := &feedbackStub{harness: "amp", err: errors.New("unconfirmed")}
	lookups := 0
	m := testNewModel(t, plainRenderer(), annotation.NewStore(), noopHighlighter(), ModelConfig{
		Harnesses: map[string]func() (FeedbackSender, error){"amp": func() (FeedbackSender, error) {
			lookups++
			return sender, nil
		}},
	})
	m.store.Add(annotation.Annotation{File: "review.go", Line: 8, Comment: "send once"})
	model, lookup := m.connectHarness("amp")
	m = model.(Model)
	m.startCommand()
	m.command.input.SetValue("harness send")
	model, duplicate := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(Model)
	require.Nil(t, duplicate, "send joins the manual lookup, even without automatic discovery")
	require.Equal(t, discoveryForSend, m.live.discovery)
	model, send := m.Update(lookup())
	m = model.(Model)
	model, _ = m.Update(send().(tea.BatchMsg)[0]())
	m = model.(Model)
	require.Equal(t, 1, lookups)
	require.Len(t, sender.content, 1)
	require.Equal(t, 1, m.store.Count(), "unconfirmed sends retain annotations")
	model, cmd := m.connectHarness("amp")
	m = model.(Model)
	require.Nil(t, cmd, "connect cannot discard the bound session's retry cache")
	sender.err = nil
	m.startCommand()
	m.command.input.SetValue("harness send")
	model, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(Model)
	model, _ = m.Update(cmd())
	m = model.(Model)
	require.Equal(t, []string{sender.content[0], sender.content[0]}, sender.content)
	require.Zero(t, m.store.Count())
}

func TestFeedbackDiscoveryAfterLaunchPreservesDrafts(t *testing.T) {
	var available FeedbackSender
	store := annotation.NewStore()
	store.Add(annotation.Annotation{File: "a.go", Line: 7, Comment: "keep this"})
	m := testNewModel(t, plainRenderer(), store, noopHighlighter(), ModelConfig{
		DiscoverFeedback: func() (FeedbackSender, error) { return available, nil },
	})
	require.NotNil(t, m.feedbackTick())
	require.Nil(t, m.liveTick(), "do not refresh files before connecting")
	model, cmd := m.handleFlushOutput()
	m = model.(Model)
	model, next := m.Update(cmd())
	m = model.(Model)
	require.Nil(t, next)
	require.Contains(t, m.output.hint, "Harness not connected")
	require.NotContains(t, m.output.hint, "--output")
	require.Equal(t, 1, store.Count())
	require.Equal(t, discoveryIdle, m.live.discovery, "a failed lookup must not leave a queued send")

	sender := &feedbackStub{}
	available = sender
	m.annot.annotating = true
	model, cmd = m.Update(feedbackTickMsg{})
	m = model.(Model)
	lookup := cmd().(tea.BatchMsg)[0] // the other command schedules another discovery tick
	model, next = m.Update(lookup())
	m = model.(Model)
	require.Same(t, sender, m.live.sender)
	require.True(t, m.annot.annotating)
	require.Equal(t, 1, store.Count())
	require.Equal(t, "Harness connected; press O to send feedback", m.output.hint)
	require.Empty(t, sender.content, "background connection must not send annotations")
	require.NotNil(t, next, "start live refresh after connecting")
	require.Nil(t, m.feedbackTick(), "stop discovery once bound")
	_, next = m.Update(feedbackTickMsg{})
	require.Nil(t, next, "an already scheduled discovery tick must stop too")
}

func TestFlushJoinsInFlightDiscoveryAndSendsOnce(t *testing.T) {
	sender := &feedbackStub{}
	lookups := 0
	store := annotation.NewStore()
	store.Add(annotation.Annotation{File: "review.md", Line: 12, Comment: "late connection"})
	m := testNewModel(t, plainRenderer(), store, noopHighlighter(), ModelConfig{
		DiscoverFeedback: func() (FeedbackSender, error) { lookups++; return sender, nil },
	})
	model, lookup := m.discoverFeedback(false)
	m = model.(Model)
	require.Equal(t, discoveryBackground, m.live.discovery)
	for range 2 {
		var cmd tea.Cmd
		model, cmd = m.handleFlushOutput()
		m = model.(Model)
		require.Nil(t, cmd, "O joins rather than duplicates an in-flight lookup")
		require.Equal(t, discoveryForSend, m.live.discovery)
	}
	model, duplicate := m.discoverFeedback(false)
	m = model.(Model)
	require.Nil(t, duplicate)
	require.Equal(t, discoveryForSend, m.live.discovery, "background tick must not downgrade a queued send")
	model, cmd := m.Update(lookup())
	m = model.(Model)
	require.Equal(t, discoveryIdle, m.live.discovery)
	require.Equal(t, liveSending, m.live.operation)
	commands := cmd().(tea.BatchMsg)
	model, _ = m.Update(commands[0]()) // send; the other command schedules live refresh
	m = model.(Model)
	require.Equal(t, 1, lookups)
	require.Len(t, sender.content, 1)
	require.Contains(t, sender.content[0], "review.md:12")
	require.Contains(t, sender.content[0], "late connection")
	require.Zero(t, store.Count())
	require.Equal(t, liveIdle, m.live.operation)
	require.Equal(t, "Feedback sent", m.output.hint)
}

func TestFeedbackDiscoveryFailureRetainsAnnotations(t *testing.T) {
	store := annotation.NewStore()
	store.Add(annotation.Annotation{File: "a.go", Line: 3, Comment: "keep"})
	m := testNewModel(t, plainRenderer(), store, noopHighlighter(), ModelConfig{
		DiscoverFeedback: func() (FeedbackSender, error) { return nil, errors.New("multiple Amp sessions") },
	})
	model, cmd := m.handleFlushOutput()
	m = model.(Model)
	model, _ = m.Update(cmd())
	m = model.(Model)
	require.Equal(t, "multiple Amp sessions", m.output.hint)
	require.Nil(t, m.live.sender)
	require.Equal(t, liveIdle, m.live.operation)
	require.Equal(t, discoveryIdle, m.live.discovery)
	require.Equal(t, 1, store.Count())
}

func TestFeedbackRetryPreservesNewAndEditedAnnotations(t *testing.T) {
	sender := &feedbackStub{err: errors.New("offline")}
	store := annotation.NewStore()
	a := annotation.Annotation{File: "a.go", Line: 3, Type: "+", Comment: "first"}
	b := annotation.Annotation{File: "a.go", Line: 9, Type: "+", Comment: "second"}
	store.Add(a)
	store.Add(b)
	m := testNewModel(t, plainRenderer(), store, noopHighlighter(), ModelConfig{
		Feedback:         sender,
		DiscoverFeedback: func() (FeedbackSender, error) { t.Fatal("must not switch threads on send failure"); return nil, nil },
	})
	model, cmd := m.handleFlushOutput()
	m = model.(Model)
	require.Equal(t, liveSending, m.live.operation)
	_, duplicate := m.handleFlushOutput()
	require.Nil(t, duplicate)
	model, _ = m.Update(cmd())
	m = model.(Model)
	require.Equal(t, 2, store.Count())
	require.Error(t, m.live.err)
	require.Equal(t, liveIdle, m.live.operation)
	require.NotEmpty(t, m.live.pending, "failed sends stay retryable after returning to idle")
	a.Comment = "edited during delivery"
	store.Add(a)
	store.Add(annotation.Annotation{File: "b.go", Line: 1, Comment: "new"})
	sender.err = nil
	model, cmd = m.handleFlushOutput()
	m = model.(Model)
	model, _ = m.Update(cmd())
	m = model.(Model)
	require.Equal(t, sender.content[0], sender.content[1], "retry the exact pending snapshot")
	require.Equal(t, 2, store.Count())
	require.Equal(t, []annotation.Annotation{a}, store.Get("a.go"))
	require.Empty(t, m.live.pending)
	require.NoError(t, m.live.err)
	require.Equal(t, "Feedback sent", m.output.hint)
}

func TestLiveRefreshGuardsAndCursor(t *testing.T) {
	m := testNewModel(t, plainRenderer(), annotation.NewStore(), noopHighlighter(), ModelConfig{Feedback: &feedbackStub{}})
	entries := []diff.FileEntry{{Path: "a.go", Status: diff.FileModified}}
	old := []diff.DiffLine{{OldNum: 1, NewNum: 1, Content: "context", ChangeType: diff.ChangeContext}, {NewNum: 2, Content: "before", ChangeType: diff.ChangeAdd}}
	model, _ := m.handleFilesLoaded(filesLoadedMsg{entries: entries})
	m = model.(Model)
	model, _ = m.handleFileLoaded(fileLoadedMsg{file: "a.go", lines: old, seq: m.file.loadSeq})
	m = model.(Model)
	m.nav.diffCursor = 1
	updated := append([]diff.DiffLine(nil), old...)
	updated[1].Content = "after"
	msg := liveLoadedMsg{files: filesLoadedMsg{seq: m.filesLoadSeq, entries: entries}, file: fileLoadedMsg{seq: m.file.loadSeq, file: "a.go", lines: updated}}
	m.annot.annotating = true
	model, _ = m.handleLiveLoaded(msg)
	require.Equal(t, old, model.(Model).file.lines, "draft entered during IO must pause refresh")
	m.annot.annotating = false
	m.store.Add(annotation.Annotation{File: "a.go", Line: 2, Comment: "unsent"})
	model, _ = m.handleLiveLoaded(msg)
	require.Equal(t, old, model.(Model).file.lines)
	m.store.Clear()
	stale := msg
	stale.file.seq++
	model, _ = m.handleLiveLoaded(stale)
	require.Equal(t, old, model.(Model).file.lines)
	model, _ = m.handleLiveLoaded(msg)
	m = model.(Model)
	require.Equal(t, updated, m.file.lines)
	require.Equal(t, 1, m.nav.diffCursor)
}
