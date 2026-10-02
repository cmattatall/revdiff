package ui

import (
	"errors"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/umputun/revdiff/app/diff"
	"github.com/umputun/revdiff/app/highlight"
)

func TestHighlightingCacheHitRendersImmediately(t *testing.T) {
	m := splitTestModel(t)
	m.highlighter = highlight.New("monokai", true)
	lines := []diff.DiffLine{{Content: "package main", ChangeType: diff.ChangeContext}}
	want := m.highlighter.HighlightLines("partial.go", lines)
	model, cmd := m.handleFileLoaded(fileLoadedMsg{file: "partial.go", seq: m.file.loadSeq, lines: lines})
	m = model.(Model)
	require.Nil(t, cmd, "cached highlighting must not schedule another tokenizer or second render")
	require.Equal(t, want, m.file.highlighted)
	lines = []diff.DiffLine{{Content: "package changed", ChangeType: diff.ChangeContext}}
	model, cmd = m.handleFileLoaded(fileLoadedMsg{file: "partial.go", seq: m.file.loadSeq, lines: lines})
	require.NotNil(t, cmd, "an edit must miss the cache even at the same path")
	require.Nil(t, model.(Model).file.highlighted)
	require.Contains(t, model.(Model).layout.viewport.View(), "package changed")
}

func TestHighlightingDoesNotBlockFileNavigation(t *testing.T) {
	m := splitTestModel(t)
	h := noopHighlighter()
	started, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	h.HighlightLinesFunc = func(_ string, lines []diff.DiffLine) []string {
		close(started)
		<-release
		return []string{"stale index colors"}
	}
	m.highlighter = h
	oldCmd := m.loadHighlight()
	done := make(chan tea.Msg, 1)
	go func() { done <- oldCmd() }()
	<-started // deliberately leave the old tokenizer blocked

	m.tree.(*workingTree).SelectEntry(diff.FileEntry{Path: "partial.go"})
	model, load := m.loadSelectedIfChanged()
	m = model.(Model)
	model, color := m.Update(load())
	m = model.(Model)
	require.False(t, m.file.staged)
	require.Contains(t, m.layout.viewport.View(), "needle working copy")
	require.Nil(t, m.file.highlighted, "the new diff displays before tokenization finishes")
	unblock()
	model, _ = m.Update(<-done)
	m = model.(Model)
	require.Nil(t, m.file.highlighted, "old index colors must not paint the same path's working diff")
	require.NotNil(t, color)
}

func TestHighlightingLatestOnlyAndPreservesViewport(t *testing.T) {
	m := testModel([]string{"a.go", "b.go"}, nil)
	h := noopHighlighter()
	h.HighlightLinesFunc = func(_ string, lines []diff.DiffLine) []string {
		result := make([]string, len(lines))
		for i, line := range lines {
			result[i] = "\x1b[32m" + line.Content + "\x1b[39m"
		}
		return result
	}
	m.highlighter = h
	m.file.name, m.file.lines = "a.go", benchDiffLines(80)
	obsolete := m.loadHighlight()
	latest := m.loadHighlight()
	require.Nil(t, obsolete(), "a superseded command must not even invoke the tokenizer")
	require.Empty(t, h.HighlightLinesCalls())
	msg := latest().(highlightedMsg)
	require.Len(t, h.HighlightLinesCalls(), 1)

	m.layout.viewport.SetHeight(10)
	m.layout.viewport.SetContent(m.renderDiff())
	m.layout.viewport.SetYOffset(25)
	m.nav.diffCursor = 30
	m.annot.annotating = true
	m.annot.input.SetValue("draft")
	model, _ := m.Update(msg)
	m = model.(Model)
	require.Equal(t, msg.lines, m.file.highlighted)
	require.Equal(t, 25, m.layout.viewport.YOffset())
	require.Equal(t, 30, m.nav.diffCursor)
	require.Equal(t, "draft", m.annot.input.Value())
}

func TestHighlightingInvalidation(t *testing.T) {
	for _, invalidation := range []string{"theme", "empty tree", "error"} {
		t.Run(invalidation, func(t *testing.T) {
			current := testModel([]string{"a.go", "b.go"}, nil)
			h := noopHighlighter()
			h.HighlightLinesFunc = func(string, []diff.DiffLine) []string { return []string{"old colors"} }
			current.highlighter = h
			current.file.name = "a.go"
			current.file.lines = []diff.DiffLine{{Content: "old content", ChangeType: diff.ChangeAdd}}
			msg := current.loadHighlight()()
			switch invalidation {
			case "theme":
				h.StyleNameFunc = func() string { return "dracula" }
			case "empty tree":
				model, _ := current.handleFilesLoaded(filesLoadedMsg{seq: current.filesLoadSeq})
				current = model.(Model)
			case "error":
				model, _ := current.handleFileLoaded(fileLoadedMsg{file: "b.go", seq: current.file.loadSeq, err: errors.New("failed")})
				current = model.(Model)
			}
			before := current.layout.viewport.View()
			model, _ := current.Update(msg)
			require.Equal(t, before, model.(Model).layout.viewport.View())
		})
	}
}
