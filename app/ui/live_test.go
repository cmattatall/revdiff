package ui

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/umputun/revdiff/app/annotation"
	"github.com/umputun/revdiff/app/diff"
)

type feedbackStub struct {
	content []string
	err     error
}

func (s *feedbackStub) Send(content string) error {
	s.content = append(s.content, content)
	return s.err
}

func TestFeedbackRetryPreservesNewAndEditedAnnotations(t *testing.T) {
	sender := &feedbackStub{err: errors.New("offline")}
	store := annotation.NewStore()
	a := annotation.Annotation{File: "a.go", Line: 3, Type: "+", Comment: "first"}
	b := annotation.Annotation{File: "a.go", Line: 9, Type: "+", Comment: "second"}
	store.Add(a)
	store.Add(b)
	m := testNewModel(t, plainRenderer(), store, noopHighlighter(), ModelConfig{Feedback: sender})
	model, cmd := m.handleFlushOutput()
	m = model.(Model)
	require.True(t, m.live.sending)
	_, duplicate := m.handleFlushOutput()
	require.Nil(t, duplicate)
	model, _ = m.Update(cmd())
	m = model.(Model)
	require.Equal(t, 2, store.Count())
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
	require.Equal(t, "Feedback sent to Amp", m.output.hint)
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
