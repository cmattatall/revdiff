package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/umputun/revdiff/app/diff"
	"github.com/umputun/revdiff/app/ui/sidepane"
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
