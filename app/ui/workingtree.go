package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/umputun/revdiff/app/diff"
	"github.com/umputun/revdiff/app/ui/sidepane"
	"github.com/umputun/revdiff/app/ui/style"
)

// workingTree presents two independently scrolling trees through the existing
// tree contract. active identifies the half keyboard actions address.
type workingTree struct {
	staged, changes FileTreeComponent
	activeStaged    bool
	height          int
}

func newWorkingTree(factory func([]diff.FileEntry) FileTreeComponent) *workingTree {
	return &workingTree{staged: factory(nil), changes: factory(nil), activeStaged: true}
}
func (w *workingTree) active() FileTreeComponent {
	if w.activeStaged {
		return w.staged
	}
	return w.changes
}
func (w *workingTree) SelectedStaged() bool { return w.activeStaged }
func (w *workingTree) SelectedFile() string { return w.active().SelectedFile() }
func (w *workingTree) VisibleFiles() []string {
	return append(w.staged.VisibleFiles(), w.changes.VisibleFiles()...)
}
func (w *workingTree) VisibleEntries() []diff.FileEntry {
	result := make([]diff.FileEntry, 0, len(w.VisibleFiles()))
	for _, p := range w.staged.VisibleFiles() {
		result = append(result, diff.FileEntry{Path: p, OldPath: w.staged.OldPath(p), Status: w.staged.FileStatus(p), Staged: true})
	}
	for _, p := range w.changes.VisibleFiles() {
		result = append(result, diff.FileEntry{Path: p, OldPath: w.changes.OldPath(p), Status: w.changes.FileStatus(p)})
	}
	return result
}
func (w *workingTree) SelectEntry(e diff.FileEntry) bool {
	if !w.side(e.Staged).SelectByPath(e.Path) {
		return false
	}
	w.activeStaged = e.Staged
	return true
}
func (w *workingTree) side(staged bool) FileTreeComponent {
	if staged {
		return w.staged
	}
	return w.changes
}
func (w *workingTree) SelectedVisibleIndex() int {
	files := w.active().VisibleFiles()
	for i, p := range files {
		if p == w.active().SelectedFile() {
			if !w.activeStaged {
				return len(w.staged.VisibleFiles()) + i
			}
			return i
		}
	}
	return 0
}
func (w *workingTree) TotalFiles() int                     { return w.staged.TotalFiles() + w.changes.TotalFiles() }
func (w *workingTree) FileStatus(p string) diff.FileStatus { return w.active().FileStatus(p) }
func (w *workingTree) OldPath(p string) string             { return w.active().OldPath(p) }
func (w *workingTree) FilterActive() bool                  { return w.active().FilterActive() }
func (w *workingTree) UnreviewedFilterActive() bool        { return w.active().UnreviewedFilterActive() }
func (w *workingTree) ReviewedCount() int {
	return w.staged.ReviewedCount() + w.changes.ReviewedCount()
}
func (w *workingTree) ReviewedFingerprints() map[string]string {
	result := w.changes.ReviewedFingerprints()
	for path, fingerprint := range w.staged.ReviewedFingerprints() {
		result[reviewKey(path, true)] = fingerprint
	}
	return result
}

// NUL cannot appear in a filesystem path, so the index namespace cannot collide.
func reviewKey(path string, staged bool) string {
	if staged {
		return "\x00" + path
	}
	return path
}
func (w *workingTree) IsReviewed(p string) bool { return w.active().IsReviewed(p) }
func (w *workingTree) HasFile(d sidepane.Direction) bool {
	return w.active().HasFile(d) || (d == sidepane.DirectionNext && w.activeStaged && len(w.changes.VisibleFiles()) > 0) ||
		(d == sidepane.DirectionPrev && !w.activeStaged && len(w.staged.VisibleFiles()) > 0)
}
func (w *workingTree) Move(m sidepane.Motion, n ...int) {
	switch m {
	case sidepane.MotionDown, sidepane.MotionPageDown:
		if w.activeStaged && !w.staged.HasFile(sidepane.DirectionNext) {
			w.activeStaged = false
			w.changes.Move(sidepane.MotionFirst)
			return
		}
	case sidepane.MotionUp, sidepane.MotionPageUp:
		if !w.activeStaged && !w.changes.HasFile(sidepane.DirectionPrev) {
			w.activeStaged = true
			w.staged.Move(sidepane.MotionLast)
			return
		}
	case sidepane.MotionFirst:
		w.activeStaged = len(w.staged.VisibleFiles()) > 0
	case sidepane.MotionLast:
		w.activeStaged = len(w.changes.VisibleFiles()) == 0
	}
	if len(n) > 0 {
		top, bottom := w.bodyHeights(w.height)
		height := bottom
		if w.activeStaged {
			height = top
		}
		n = []int{max(1, min(n[0], height))}
	}
	w.active().Move(m, n...)
}
func (w *workingTree) StepFile(d sidepane.Direction) {
	if w.active().HasFile(d) {
		w.active().StepFile(d)
		return
	}
	other := w.side(!w.activeStaged)
	if len(other.VisibleFiles()) > 0 {
		w.activeStaged = !w.activeStaged
	}
	if d == sidepane.DirectionNext {
		w.active().Move(sidepane.MotionFirst)
	} else {
		w.active().Move(sidepane.MotionLast)
	}
}
func (w *workingTree) SelectByPath(p string) bool {
	if w.active().SelectByPath(p) {
		return true
	}
	return w.SelectEntry(diff.FileEntry{Path: p, Staged: !w.activeStaged})
}
func (w *workingTree) SelectByVisibleRow(row int) bool {
	top, bottom := w.bodyHeights(w.height)
	if row < 0 || row >= w.height || row == top+1 {
		return false
	}
	if row <= top {
		if row != 0 && !w.staged.SelectByVisibleRow(row-1) {
			return false
		}
		w.activeStaged = true
		return true
	}
	row -= top + 2
	if row > bottom || (row != 0 && !w.changes.SelectByVisibleRow(row-1)) {
		return false
	}
	w.activeStaged = false
	return true
}
func (w *workingTree) bodyHeights(height int) (int, int) {
	available := max(0, height-3) // two titles and a blank separator
	return available / 2, available - available/2
}
func (w *workingTree) EnsureVisible(h int) {
	w.height = h
	top, bottom := w.bodyHeights(h)
	w.staged.EnsureVisible(top)
	w.changes.EnsureVisible(bottom)
}
func (w *workingTree) Rebuild(es []diff.FileEntry) {
	stagedPath, changesPath := w.staged.SelectedFile(), w.changes.SelectedFile()
	var a, b []diff.FileEntry
	for _, e := range es {
		if e.Staged {
			a = append(a, e)
		} else {
			b = append(b, e)
		}
	}
	w.staged.Rebuild(a)
	w.changes.Rebuild(b)
	w.staged.SelectByPath(stagedPath)
	w.changes.SelectByPath(changesPath)
	if w.active().SelectedFile() == "" {
		w.activeStaged = w.staged.TotalFiles() > 0
	}
}
func (w *workingTree) ToggleFilter(a map[string]bool) {
	w.staged.ToggleFilter(a)
	w.changes.ToggleFilter(a)
}
func (w *workingTree) ToggleUnreviewedFilter() {
	w.staged.ToggleUnreviewedFilter()
	w.changes.ToggleUnreviewedFilter()
}
func (w *workingTree) RefreshFilter(a map[string]bool) {
	w.staged.RefreshFilter(a)
	w.changes.RefreshFilter(a)
}
func (w *workingTree) RefreshUnreviewedFilter() {
	w.staged.RefreshUnreviewedFilter()
	w.changes.RefreshUnreviewedFilter()
}
func (w *workingTree) SetReviewed(p, f string) { w.active().SetReviewed(p, f) }
func (w *workingTree) Unreview(p string)       { w.active().Unreview(p) }
func (w *workingTree) ReconcileReviewed(a, b map[string]string) {
	before, current := make(map[string]string), make(map[string]string)
	for key, value := range a {
		if path, staged := strings.CutPrefix(key, "\x00"); staged {
			before[path] = value
			current[path] = b[key]
		}
	}
	w.staged.ReconcileReviewed(before, current)
	w.changes.ReconcileReviewed(a, b)
}
func (w *workingTree) ReconcileReviewedPath(p, f string) { w.active().ReconcileReviewedPath(p, f) }

// Each half scrolls independently; a full-height scrollbar would be misleading.
func (w *workingTree) ScrollState() sidepane.ScrollState { return sidepane.ScrollState{} }
func (w *workingTree) Render(r sidepane.FileTreeRender) string {
	w.height = r.Height
	top, bottom := w.bodyHeights(r.Height)
	var rows []string
	for _, section := range []struct {
		name   string
		tree   FileTreeComponent
		height int
		active bool
	}{{"Staged", w.staged, top, w.activeStaged}, {"Changes", w.changes, bottom, !w.activeStaged}} {
		if len(rows) > 0 {
			rows = append(rows, "")
		}
		label := fmt.Sprintf(" %s (%d)", section.name, section.tree.TotalFiles())
		if section.active {
			label = ">" + label[1:]
		}
		color := r.Resolver.Color(style.ColorKeyModifyLineFg)
		if section.tree == w.staged {
			color = r.Resolver.Color(style.ColorKeyAddLineFg)
		}
		label = ansi.Truncate(label, r.Width, "")
		if color != "" {
			label = string(color) + label + string(style.ResetFg)
		}
		rows = append(rows, lipgloss.NewStyle().Bold(true).Render(label))
		if section.height == 0 {
			continue
		}
		sr := r
		sr.Height = section.height
		sr.HideSelection = !section.active
		body := strings.Split(section.tree.Render(sr), "\n")
		for i := range section.height {
			line := ""
			if i < len(body) {
				line = ansi.Truncate(body[i], r.Width, "")
			}
			rows = append(rows, line)
		}
	}
	return lipgloss.NewStyle().MaxHeight(r.Height).Render(strings.Join(rows, "\n"))
}
