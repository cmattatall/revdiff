package ui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/umputun/revdiff/app/annotation"
	"github.com/umputun/revdiff/app/diff"
	"github.com/umputun/revdiff/app/keymap"
	"github.com/umputun/revdiff/app/ui/mocks"
	"github.com/umputun/revdiff/app/ui/sidepane"
	"github.com/umputun/revdiff/app/ui/style"
)

func TestWorkingTreeKeepsPartialPathSidesDistinct(t *testing.T) {
	w := newWorkingTree(testFileTreeFactory())
	w.Rebuild([]diff.FileEntry{
		{Path: "partial.go", Status: diff.FileModified, Staged: true},
		{Path: "partial.go", Status: diff.FileModified},
		{Path: "new.go", Status: diff.FileUntracked},
	})
	require.Equal(t, []string{"partial.go", "new.go", "partial.go"}, w.VisibleFiles())
	assert.True(t, w.SelectedStaged())

	w.Move(sidepane.MotionDown) // end of Staged crosses into Changes
	assert.False(t, w.SelectedStaged())
	assert.Equal(t, "new.go", w.SelectedFile())
	assert.True(t, w.SelectEntry(diff.FileEntry{Path: "partial.go"}))
	assert.False(t, w.SelectedStaged())
}

func TestWorkingTreeEmptyHalfRemainsRendered(t *testing.T) {
	w := newWorkingTree(testFileTreeFactory())
	w.Rebuild([]diff.FileEntry{{Path: "staged.go", Staged: true}})
	m := testModel(nil, nil)
	got := w.Render(sidepane.FileTreeRender{Width: 30, Height: 10, Resolver: m.resolver, Renderer: m.renderer})
	assert.Contains(t, got, "Staged")
	assert.Contains(t, got, "Changes")
}

func TestWorkingTreeHeadingColors(t *testing.T) {
	w := newWorkingTree(testFileTreeFactory())
	w.Rebuild([]diff.FileEntry{{Path: "src/a.go", Staged: true}, {Path: "src/b.go"}})
	m := testModel(nil, nil)
	res := style.NewResolver(style.Colors{Accent: "#112233", AddFg: "#44aa55", ModifyFg: "#ccbb66"})
	rows := strings.Split(w.Render(sidepane.FileTreeRender{Width: 30, Height: 13, Resolver: res, Renderer: m.renderer}), "\n")
	require.Contains(t, rows[0], "\x1b[38;2;68;170;85m>Staged (1)")
	require.Contains(t, rows[7], "\x1b[38;2;204;187;102m Changes (1)")
	require.NotContains(t, rows[1], "\x1b[38;2;68;170;85m", "heading color must not leak into folders")
	plain := w.Render(sidepane.FileTreeRender{Width: 30, Height: 13, Resolver: style.PlainResolver(), Renderer: m.renderer})
	require.NotContains(t, plain, "38;2;", "respect no-colors mode")
}

func TestWorkingTreeLayoutAndMouseRows(t *testing.T) {
	w := newWorkingTree(testFileTreeFactory())
	w.Rebuild([]diff.FileEntry{{Path: "a.go", Staged: true}, {Path: "b.go"}, {Path: "c.go"}})
	m := testModel(nil, nil)
	for _, height := range []int{13, 14} {
		view := ansi.Strip(w.Render(sidepane.FileTreeRender{Width: 30, Height: height, Resolver: m.resolver, Renderer: m.renderer}))
		rows := strings.Split(view, "\n")
		top := (height - 3) / 2
		require.Len(t, rows, height)
		require.Contains(t, rows[0], "Staged (1)")
		require.Contains(t, rows[1], "./", "Staged keeps its existing layout")
		require.Empty(t, strings.TrimSpace(rows[top+1]), "a blank row separates fixed-height regions")
		require.Contains(t, rows[top+2], "Changes (2)")
		require.Contains(t, rows[top+3], "b.go", "root files follow Changes directly")
		require.NotContains(t, strings.Join(rows[top+3:], "\n"), "./")
		require.True(t, w.SelectByVisibleRow(top+3)) // first file immediately after Changes heading
		require.Equal(t, "b.go", w.SelectedFile())
		require.False(t, w.SelectedStaged())
		require.False(t, w.SelectByVisibleRow(top+1), "separator is not a selectable row")
		require.True(t, w.SelectByVisibleRow(2))
		require.True(t, w.SelectedStaged())
		require.Equal(t, "a.go", w.SelectedFile())
	}
}

func splitTestModel(t *testing.T) Model {
	t.Helper()
	renderer := &mocks.RendererMock{
		ChangedFilesFunc: func(_ string, _ bool) ([]diff.FileEntry, error) {
			return []diff.FileEntry{{Path: "partial.go", Status: diff.FileModified}}, nil
		},
		FileDiffFunc: func(req diff.FileDiffRequest) ([]diff.DiffLine, error) {
			if req.Staged {
				return []diff.DiffLine{{NewNum: 1, Content: "needle index", ChangeType: diff.ChangeAdd}}, nil
			}
			return []diff.DiffLine{
				{NewNum: 1, Content: "ordinary", ChangeType: diff.ChangeContext},
				{NewNum: 2, Content: "needle working copy", ChangeType: diff.ChangeAdd},
			}, nil
		},
	}
	m := testNewModel(t, renderer, annotation.NewStore(), noopHighlighter(), ModelConfig{WorkingTree: true, TreeWidthRatio: 3})
	model, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = model.(Model)
	model, _ = m.handleFilesLoaded(m.loadFiles()().(filesLoadedMsg))
	m = model.(Model)
	model, _ = m.handleFileLoaded(m.loadFileDiff(m.tree.SelectedFile())().(fileLoadedMsg))
	return model.(Model)
}

func TestWorkingTreeSamePathLoadsAndCancelsBySide(t *testing.T) {
	m := splitTestModel(t)
	w := m.tree.(*workingTree)
	require.False(t, m.file.singleFile)
	require.True(t, m.file.staged)
	w.SelectEntry(diff.FileEntry{Path: "partial.go"})
	model, cmd := m.loadSelectedIfChanged()
	m = model.(Model)
	require.NotNil(t, cmd)
	stale := cmd().(fileLoadedMsg)
	require.False(t, stale.staged)
	w.SelectEntry(diff.FileEntry{Path: "partial.go", Staged: true})
	model, _ = m.loadSelectedIfChanged()
	m = model.(Model)
	model, _ = m.handleFileLoaded(stale)
	m = model.(Model)
	require.True(t, m.file.staged)
	require.Equal(t, "needle index", m.file.lines[0].Content)
	w.SelectEntry(diff.FileEntry{Path: "partial.go"})
	model, cmd = m.loadSelectedIfChanged()
	m = model.(Model)
	model, _ = m.handleFileLoaded(cmd().(fileLoadedMsg))
	m = model.(Model)
	require.False(t, m.file.staged)
	require.Equal(t, "needle working copy", m.file.lines[1].Content)
}

func TestWorkingTreeSearchAndReviewedStateUseBothSides(t *testing.T) {
	m := splitTestModel(t)
	m.layout.focus = paneTree
	model, _ := m.handleMarkReviewed()
	m = model.(Model)
	w := m.tree.(*workingTree)
	require.True(t, w.staged.IsReviewed("partial.go"))
	require.False(t, w.changes.IsReviewed("partial.go"))
	m.search.term = "needle"
	for _, staged := range []bool{false, true} {
		cmd := m.scanTree(treeScanSearch, true, false)
		require.NotNil(t, cmd)
		model, _ = m.handleTreeScan(cmd().(treeScanMsg))
		m = model.(Model)
		require.Equal(t, staged, m.file.staged)
		if staged {
			require.Equal(t, "needle index", m.file.lines[m.nav.diffCursor].Content)
		} else {
			require.Equal(t, "needle working copy", m.file.lines[m.nav.diffCursor].Content)
		}
	}
	// A Changes edit does not revoke the index's independent reviewed mark.
	msg := m.loadFiles()().(filesLoadedMsg)
	require.Len(t, msg.reviewedFingerprints, 1)
	model, _ = m.handleFilesLoaded(msg)
	m = model.(Model)
	require.True(t, w.staged.IsReviewed("partial.go"))
	require.False(t, w.changes.IsReviewed("partial.go"))
}

func TestWorkingTreeStagingAndUntrackedReplacement(t *testing.T) {
	m := splitTestModel(t)
	calls := 0
	m.live.stager = stagerStub{unstageFile: func(path, oldPath string) error {
		calls++
		require.Equal(t, "partial.go", path)
		return nil
	}}
	model, cmd := m.handleStage(keymap.ActionStageFile)
	require.NotNil(t, cmd)
	require.Zero(t, calls)
	require.True(t, cmd().(stagedMsg).unstage)
	require.Equal(t, 1, calls)
	require.Equal(t, "Unstaging file", model.(Model).output.hint)
	m.loadUntracked = func() ([]string, error) { return []string{"replacement.go"}, nil }
	m.diffRenderer.(*mocks.RendererMock).ChangedFilesFunc = func(_ string, staged bool) ([]diff.FileEntry, error) {
		if staged {
			return []diff.FileEntry{{Path: "replacement.go", Status: diff.FileDeleted}}, nil
		}
		return nil, nil
	}
	msg := m.loadFiles()().(filesLoadedMsg)
	require.NoError(t, msg.err)
	require.ElementsMatch(t, []diff.FileEntry{
		{Path: "replacement.go", Status: diff.FileDeleted, Staged: true},
		{Path: "replacement.go", Status: diff.FileUntracked},
	}, msg.entries)
}

func TestWholeFileStagingReturnsToNextChange(t *testing.T) {
	for _, tc := range []struct {
		name, path, want string
		hidden, fail     bool
		unstage          bool
	}{
		{name: "middle file", path: "b.go", want: "c.go"},
		{name: "last file wraps", path: "c.go", want: "a.go"},
		{name: "hidden tree reopens", path: "b.go", want: "c.go", hidden: true},
		{name: "failed stage stays put", path: "b.go", want: "b.go", fail: true},
		{name: "unstage middle file", path: "b.go", want: "c.go", unstage: true},
		{name: "unstage last file wraps", path: "c.go", want: "a.go", unstage: true},
		{name: "unstage hidden tree reopens", path: "b.go", want: "c.go", unstage: true, hidden: true},
		{name: "failed unstage stays put", path: "b.go", want: "b.go", unstage: true, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := splitTestModel(t)
			entries := []diff.FileEntry{
				{Path: tc.path, Status: diff.FileModified, Staged: !tc.unstage},
				{Path: "a.go", Status: diff.FileModified, Staged: tc.unstage},
				{Path: "b.go", Status: diff.FileModified, Staged: tc.unstage},
				{Path: "c.go", Status: diff.FileModified, Staged: tc.unstage},
			}
			model, _ := m.handleFilesLoaded(filesLoadedMsg{seq: m.filesLoadSeq, entries: entries})
			m = model.(Model)
			m.tree.(*workingTree).SelectEntry(diff.FileEntry{Path: tc.path, Staged: tc.unstage})
			model, load := m.loadSelectedIfChanged()
			m = model.(Model)
			model, _ = m.Update(load())
			m = model.(Model)
			m.layout.focus = paneDiff
			if tc.hidden {
				m.toggleTreePane()
			}
			update := func(path, _ string) error {
				require.Equal(t, tc.path, path)
				if tc.fail {
					return errors.New("index locked")
				}
				return nil
			}
			stub := stagerStub{file: update}
			if tc.unstage {
				stub.file, stub.unstageFile = nil, update
			}
			m.live.stager = stub
			model, stage := m.Update(tea.KeyPressMsg{Code: 'S', Text: string('S')})
			m = model.(Model)
			require.NotNil(t, stage)
			model, reload := m.Update(stage())
			m = model.(Model)
			if tc.fail {
				require.Nil(t, reload)
				require.Equal(t, paneDiff, m.layout.focus)
				require.Equal(t, tc.want, m.tree.SelectedFile())
				return
			}
			require.NotNil(t, reload)
			var refreshed []diff.FileEntry
			for _, entry := range entries {
				if entry.Staged != tc.unstage || entry.Path != tc.path {
					refreshed = append(refreshed, entry)
				}
			}
			model, load = m.handleFilesLoaded(filesLoadedMsg{seq: m.filesLoadSeq, entries: refreshed})
			m = model.(Model)
			require.Equal(t, tc.want, m.tree.SelectedFile(), "advance from the old position, not the reset tree cursor")
			require.Equal(t, tc.unstage, m.selectedTreeStaged(), "stay in the original section")
			require.NotNil(t, load)
			model, _ = m.Update(load())
			m = model.(Model)
			require.Equal(t, tc.want, m.file.name)
			require.Equal(t, paneTree, m.layout.focus)
			require.False(t, m.treePaneHidden())
			require.Nil(t, m.live.stageAnchor)
		})
	}
}
