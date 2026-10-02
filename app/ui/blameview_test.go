package ui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/umputun/revdiff/app/diff"
	"github.com/umputun/revdiff/app/ui/overlay"
)

type lineBlameStub struct {
	request diff.LineBlameRequest
	details diff.BlameDetails
	err     error
}

func (s *lineBlameStub) FileBlame(string, string, bool) (map[int]diff.BlameLine, error) {
	return nil, nil
}

func (s *lineBlameStub) LineBlame(req diff.LineBlameRequest) (diff.BlameDetails, error) {
	s.request = req
	return s.details, s.err
}

func TestModel_BlameViewTargetsDisplayedSide(t *testing.T) {
	for _, tc := range []struct {
		command string
		removed bool
	}{{"blame view", false}, {"blame view", true}, {"bv", false}, {"bv", true}} {
		removed := tc.removed
		m := splitTestModel(t)
		m.layout.width, m.layout.height = 120, 35
		m.layout.focus = paneDiff
		m.file.name, m.file.oldName, m.file.staged = "new.go", "old.go", true
		m.file.lines = []diff.DiffLine{{OldNum: 13, NewNum: 8, ChangeType: diff.ChangeContext}}
		m.nav.diffCursor = 0
		if removed {
			m.file.lines[0].ChangeType = diff.ChangeRemove
		}
		provider := &lineBlameStub{details: diff.BlameDetails{
			BlameLine:    diff.BlameLine{Commit: strings.Repeat("a", 40), Author: "Alice", Summary: "Fix line attribution"},
			PullRequests: []string{"https://github.com/example/project/pull/42"},
		}}
		m.blamer = provider
		m.startCommand()
		m.command.input.SetValue(tc.command)
		model, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = model.(Model)
		require.NotNil(t, cmd)
		require.Equal(t, overlay.KindBlame, m.overlay.Kind())
		model, _ = m.Update(cmd())
		m = model.(Model)
		wantLine, wantPath := 8, "new.go:8"
		if removed {
			wantLine, wantPath = 13, "old.go:13"
		}
		require.Equal(t, diff.LineBlameRequest{FileDiffRequest: diff.FileDiffRequest{Path: "new.go", OldPath: "old.go", Staged: true}, Line: wantLine, Removed: removed}, provider.request)
		view := ansi.Strip(m.View().Content)
		for _, text := range []string{wantPath, "Alice", "Fix line attribution", "https://github.com/example/project/pull/42"} {
			require.Contains(t, view, text)
		}
		m.refreshInfoOverlay()
		require.Contains(t, ansi.Strip(m.View().Content), "Alice", "review-info updates cannot replace blame details")
	}
}

func TestModel_BlameViewDismissalAndStaleResults(t *testing.T) {
	m := testModel([]string{"a.go"}, nil)
	m.file.name, m.layout.focus = "a.go", paneDiff
	m.file.lines = []diff.DiffLine{{NewNum: 1, ChangeType: diff.ChangeContext}}
	m.blamer = &lineBlameStub{err: errors.New("blame unavailable")}
	model, old := m.openBlameView()
	m = model.(Model)
	m.overlay.Close()
	model, _ = m.Update(old())
	m = model.(Model)
	require.False(t, m.overlay.Active())
	model, current := m.openBlameView()
	m = model.(Model)
	model, _ = m.Update(old())
	m = model.(Model)
	require.Contains(t, ansi.Strip(m.View().Content), "Loading blame")
	model, _ = m.Update(current())
	m = model.(Model)
	require.Contains(t, ansi.Strip(m.View().Content), "blame unavailable")
	model, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = model.(Model)
	require.False(t, m.overlay.Active())
}

func TestModel_BlameViewRequiresDiffFocus(t *testing.T) {
	m := testModel([]string{"a.go"}, nil)
	m.layout.focus = paneTree
	model, cmd := m.openBlameView()
	m = model.(Model)
	require.Nil(t, cmd)
	require.Contains(t, m.keys.hint, "Focus the diff")
}
