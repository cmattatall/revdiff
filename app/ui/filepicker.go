package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/umputun/revdiff/app/diff"
	"github.com/umputun/revdiff/app/ui/overlay"
)

// openFilePicker snapshots visible working-tree paths, retaining tree filters.
func (m *Model) openFilePicker() {
	tree := m.tree
	if split, ok := tree.(*workingTree); ok {
		tree = split.changes
	}
	m.overlay.OpenFilePicker(overlay.FilePickerSpec{
		Paths:      tree.VisibleFiles(),
		ActivePath: m.file.name,
	})
}

// jumpToFile reveals path in the existing tree, focuses the diff, and uses the
// normal guarded loader. Picker choices originate from VisibleFiles, but keep
// the SelectByPath guard in case the tree changes before an outcome is handled.
func (m Model) jumpToFile(path string) (tea.Model, tea.Cmd) {
	m.pendingAnnotJump = nil
	m.nav.pendingHunkJump = nil
	if split, ok := m.tree.(*workingTree); ok {
		if !split.SelectEntry(diff.FileEntry{Path: path}) {
			return m, nil
		}
	} else if !m.tree.SelectByPath(path) {
		return m, nil
	}
	m.tree.EnsureVisible(m.treePageSize())
	m.layout.focus = paneDiff
	return m.loadSelectedIfChanged()
}
