package overlay

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/mattn/go-runewidth"

	"github.com/umputun/revdiff/app/keymap"
	"github.com/umputun/revdiff/app/ui/style"
)

const (
	filePickerMaxWidth    = 80
	filePickerMinWidth    = 24
	filePickerMargin      = 10
	filePickerBorderPad   = 4
	filePickerChromeLines = 10
)

type filePickerOverlay struct {
	all        []string
	entries    []string
	cursor     int
	offset     int
	filter     filterInput
	height     int
	popupWidth int
	heading    string
	fuzzy      bool
}

func (f *filePickerOverlay) open(spec FilePickerSpec) {
	f.heading = "files"
	f.fuzzy = false
	f.all = slices.Clone(spec.Paths)
	f.filter.open()
	f.entries = slices.Clone(f.all)
	f.cursor = 0
	f.offset = 0
	for i, path := range f.entries {
		if path == spec.ActivePath {
			f.cursor = i
			break
		}
	}
}

func (f *filePickerOverlay) applyFilter() {
	if f.filter.Value() == "" {
		f.entries = slices.Clone(f.all)
	} else {
		needle := strings.ToLower(f.filter.Value())
		f.entries = f.entries[:0]
		for _, path := range f.all {
			if f.matches(path, needle) {
				f.entries = append(f.entries, path)
			}
		}
	}
	f.cursor = 0
	f.offset = 0
}

func (f *filePickerOverlay) matches(text, needle string) bool {
	text = strings.ToLower(text)
	if !f.fuzzy {
		return strings.Contains(text, needle)
	}
	for _, r := range needle {
		at := strings.IndexRune(text, r)
		if at < 0 {
			return false
		}
		text = text[at+len(string(r)):]
	}
	return true
}

func (f *filePickerOverlay) render(ctx RenderCtx, mgr *Manager) string {
	f.height = ctx.Height
	f.popupWidth = max(min(ctx.Width-filePickerMargin, filePickerMaxWidth), filePickerMinWidth)
	maxVisible := f.maxVisible()

	if len(f.entries) == 0 {
		f.cursor = 0
		f.offset = 0
	} else {
		f.cursor = min(max(f.cursor, 0), len(f.entries)-1)
		f.offset = min(f.offset, max(len(f.entries)-maxVisible, 0))
		if f.cursor >= f.offset+maxVisible {
			f.offset = f.cursor - maxVisible + 1
		}
		if f.cursor < f.offset {
			f.offset = f.cursor
		}
	}

	contentWidth := f.popupWidth - filePickerBorderPad
	parts := []string{f.renderFilter(ctx.Resolver), ""}
	if len(f.entries) == 0 {
		muted := ctx.Resolver.Color(style.ColorKeyMutedFg)
		parts = append(parts, string(muted)+"  no matches"+string(style.ResetFg))
	} else {
		end := min(len(f.entries), f.offset+maxVisible)
		for i := f.offset; i < end; i++ {
			parts = append(parts, f.formatEntry(f.entries[i], contentWidth, i == f.cursor, ctx.Resolver))
		}
	}

	title := fmt.Sprintf(" %s (%d) ", style.SanitizeFilenameForDisplay(f.heading), len(f.all))
	if f.filter.Value() != "" {
		title = fmt.Sprintf(" %s (%d/%d) ", style.SanitizeFilenameForDisplay(f.heading), len(f.entries), len(f.all))
	}
	box := ctx.Resolver.Style(style.StyleKeyFilePickerBox).Width(f.popupWidth).Render(strings.Join(parts, "\n"))
	box = mgr.injectBorderTitle(box, title, borderEdgeText{
		accentFg: string(ctx.Resolver.Color(style.ColorKeyAccentFg)),
		paneBg:   string(ctx.Resolver.Color(style.ColorKeyDiffPaneBg)),
	})
	return box
}

func (f *filePickerOverlay) renderFilter(resolver Resolver) string {
	return f.filter.render(f.popupWidth-filePickerBorderPad, resolver)
}

func (f *filePickerOverlay) formatEntry(path string, width int, selected bool, resolver Resolver) string {
	clean := style.SanitizeFilenameForDisplay(path)
	display := f.truncateFilePath(clean, width-2)
	if selected {
		entryStyle := resolver.Style(style.StyleKeyFileSelected)
		styled := entryStyle.Render("> " + display)
		if w := lipgloss.Width(styled); w < width {
			styled += entryStyle.Render(strings.Repeat(" ", width-w))
		}
		return styled
	}
	normal := string(resolver.Color(style.ColorKeyNormalFg))
	if normal == "" {
		return "  " + display
	}
	return "  " + normal + display + string(style.ResetFg)
}

// truncateFilePath trims from the directory side so the basename remains
// visible. If the basename alone is too wide, its rightmost cells are kept.
func (f *filePickerOverlay) truncateFilePath(path string, width int) string {
	if width <= 0 {
		return ""
	}
	if runewidth.StringWidth(path) <= width {
		return path
	}
	base := filepath.Base(path)
	candidate := "…/" + base
	if runewidth.StringWidth(candidate) <= width {
		return candidate
	}
	return style.TruncateLeftToWidth(base, width)
}

func (f *filePickerOverlay) maxVisible() int {
	return max(min(len(f.entries), f.height-filePickerChromeLines), 1)
}

func (f *filePickerOverlay) handleKey(msg tea.KeyPressMsg, action keymap.Action) Outcome {
	// An explicitly bound Alt shortcut toggles the picker; ordinary printable
	// keys still filter, and deletion keys keep their text-editing behavior.
	if msg.Mod == tea.ModAlt && unicode.IsPrint(msg.Code) && action == keymap.ActionJumpFile {
		return Outcome{Kind: OutcomeClosed}
	}
	before := f.filter.Value()
	if handled, cmd := f.filter.handleKey(msg); handled {
		if f.filter.Value() != before {
			f.applyFilter()
		}
		return Outcome{Kind: OutcomeNone, Cmd: cmd}
	}
	if action == keymap.ActionJumpFile {
		return Outcome{Kind: OutcomeClosed}
	}
	if action == keymap.ActionUp {
		f.moveCursorBy(-1)
		return Outcome{Kind: OutcomeNone}
	}
	if action == keymap.ActionDown {
		f.moveCursorBy(1)
		return Outcome{Kind: OutcomeNone}
	}

	switch msg.String() {
	case "enter":
		return f.chooseCurrent()
	case "esc":
		if f.filter.Value() == "" {
			return Outcome{Kind: OutcomeClosed}
		}
		f.filter.Reset()
		f.applyFilter()
		return Outcome{Kind: OutcomeNone}
	default:
		return Outcome{Kind: OutcomeNone}
	}
}

func (f *filePickerOverlay) chooseCurrent() Outcome {
	if len(f.entries) == 0 || f.cursor < 0 || f.cursor >= len(f.entries) {
		return Outcome{Kind: OutcomeNone}
	}
	return Outcome{Kind: OutcomeFileChosen, FileChoice: &FileChoice{Path: f.entries[f.cursor]}}
}

func (f *filePickerOverlay) moveCursorBy(delta int) {
	if len(f.entries) == 0 {
		return
	}
	target := min(max(f.cursor+delta, 0), len(f.entries)-1)
	if target == f.cursor {
		return
	}
	f.cursor = target
	if f.cursor < f.offset {
		f.offset = f.cursor
	}
	if maxVisible := f.maxVisible(); f.cursor >= f.offset+maxVisible {
		f.offset = f.cursor - maxVisible + 1
	}
}

func (f *filePickerOverlay) handleMouse(event tea.MouseMsg) Outcome {
	switch event.(type) {
	case tea.MouseClickMsg, tea.MouseWheelMsg:
	default:
		return Outcome{Kind: OutcomeNone}
	}
	msg := event.Mouse()
	switch msg.Button {
	case tea.MouseWheelDown:
		f.moveCursorBy(f.wheelStep(msg.Mod.Contains(tea.ModShift)))
	case tea.MouseWheelUp:
		f.moveCursorBy(-f.wheelStep(msg.Mod.Contains(tea.ModShift)))
	case tea.MouseLeft:
		return f.handleLeftClick(msg.X, msg.Y)
	default:
		// other mouse buttons and horizontal wheels are intentionally ignored
	}
	return Outcome{Kind: OutcomeNone}
}

func (f *filePickerOverlay) wheelStep(shift bool) int {
	if !shift {
		return 1
	}
	return max(f.maxVisible()/2, 1)
}

func (f *filePickerOverlay) handleLeftClick(localX, localY int) Outcome {
	// layout inside the box: y=0 border, y=1 top padding, y=2 filter,
	// y=3 blank separator, y=4+ entries. horizontally: x=0 border, x=1 left
	// padding, x in [2, popupWidth-2) content, x=popupWidth-2 right padding,
	// x=popupWidth-1 right border. clicks outside the content rectangle are
	// no-ops so users cannot accidentally select a file by clicking chrome.
	const entriesTop = 4      // border (1) + top padding (1) + filter (1) + blank (1)
	const horizChromeCols = 2 // border (1) + side padding (1) on each side
	if localX < horizChromeCols || localX >= f.popupWidth-horizChromeCols {
		return Outcome{Kind: OutcomeNone}
	}
	relativeRow := localY - entriesTop
	if relativeRow < 0 || relativeRow >= f.maxVisible() {
		return Outcome{Kind: OutcomeNone}
	}
	entryIdx := f.offset + relativeRow
	if entryIdx < 0 || entryIdx >= len(f.entries) {
		return Outcome{Kind: OutcomeNone}
	}
	f.cursor = entryIdx
	return f.chooseCurrent()
}
