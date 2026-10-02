package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/umputun/revdiff/app/diff"
	"github.com/umputun/revdiff/app/ui/overlay"
)

// openFilePicker snapshots the sidebar's current visible file order. The tree
// owns filter state, so annotated-only and unreviewed-only views carry through
// without the overlay duplicating that logic.
func (m *Model) openFilePicker() {
	m.overlay.OpenFilePicker(overlay.FilePickerSpec{
		Paths:      m.tree.VisibleFiles(),
		ActivePath: m.file.name,
	})
}

// jumpToFile reveals path in the existing tree, focuses the diff, and uses the
// normal guarded loader. Picker choices originate from VisibleFiles, but keep
// the SelectByPath guard in case the tree changes before an outcome is handled.
func (m Model) jumpToFile(path string) (tea.Model, tea.Cmd) {
	m.pendingAnnotJump = nil
	m.nav.pendingHunkJump = nil
	// Prefer Changes in a split tree, even when Staged was previously active.
	selected := false
	if selector, ok := m.tree.(interface{ SelectEntry(diff.FileEntry) bool }); ok {
		selected = selector.SelectEntry(diff.FileEntry{Path: path})
	}
	if !selected && !m.tree.SelectByPath(path) {
		return m, nil
	}
	m.tree.EnsureVisible(m.treePageSize())
	m.layout.focus = paneDiff
	return m.loadSelectedIfChanged()
}
