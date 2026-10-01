package ui

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/umputun/revdiff/app/diff"
)

func TestModel_HunkScopeAndWrapFollowFocus(t *testing.T) {
	files := map[string][]diff.DiffLine{
		"a.go": {
			{ChangeType: diff.ChangeContext, Content: "context"},
			{ChangeType: diff.ChangeRemove, Content: "old"},
			{ChangeType: diff.ChangeAdd, Content: "replacement"},
			{ChangeType: diff.ChangeContext, Content: "gap"},
			{ChangeType: diff.ChangeAdd, Content: "second hunk"},
		},
		"b.go": {{ChangeType: diff.ChangeContext, Content: "no hunks"}},
		"c.go": {{ChangeType: diff.ChangeAdd, Content: "third hunk"}},
	}
	for _, collapsed := range []bool{false, true} {
		m := loadFileIntoModel(t, []string{"a.go", "b.go", "c.go"}, files)
		m.modes.collapsed.enabled = collapsed
		first := 1
		if collapsed {
			first = 2
		}
		for _, step := range []struct {
			focus     pane
			key, file string
			line      int
		}{
			{paneTree, "]", "a.go", first},
			{paneTree, "]", "a.go", 4},
			{paneDiff, "]", "a.go", first}, // local wrap, even with another changed file
			{paneDiff, "[", "a.go", 4},
			{paneTree, "]", "c.go", 0}, // skip b.go, which has no hunks
			{paneTree, "]", "a.go", first},
			{paneTree, "[", "c.go", 0},
			{paneDiff, "[", "c.go", 0},
			{paneTree, "[", "a.go", 4},
		} {
			m.layout.focus = step.focus
			m = treeSearchKey(t, m, step.key)
			require.Equal(t, step.file, m.file.name)
			require.Equal(t, step.line, m.nav.diffCursor)
			require.Equal(t, step.focus, m.layout.focus)
		}
		m.tree.ToggleFilter(map[string]bool{"a.go": true, "b.go": true})
		m = treeSearchKey(t, m, "]")
		require.Equal(t, "a.go", m.file.name, "hunk navigation respects tree filters")
		require.Equal(t, first, m.nav.diffCursor)
	}
}

func TestModel_TreeHunksWithoutChanges(t *testing.T) {
	m := loadFileIntoModel(t, []string{"a.go", "b.go"}, map[string][]diff.DiffLine{
		"a.go": {{ChangeType: diff.ChangeContext, Content: "unchanged"}},
		"b.go": nil,
	})
	m.layout.focus = paneTree
	for _, key := range []string{"]", "["} {
		m = treeSearchKey(t, m, key)
		require.Equal(t, "No hunks in file tree", m.output.hint)
		require.Equal(t, "a.go", m.file.name)
		require.Zero(t, m.nav.diffCursor)
	}
}
