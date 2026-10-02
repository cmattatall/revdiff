package overlay

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/umputun/revdiff/app/diff"
	"github.com/umputun/revdiff/app/keymap"
	"github.com/umputun/revdiff/app/ui/style"
)

// InspectionSpec shows either selectable symbols/locations or read-only text.
// Line selects a one-based source line and enables line numbers for source previews.
type InspectionSpec struct {
	Title string
	Items []string
	Text  string
	Line  int
	// Highlighted contains trusted ANSI produced from sanitized Text by the UI highlighter.
	Highlighted string
}

type inspectionOverlay struct {
	spec   InspectionSpec
	picker filePickerOverlay
	lines  []string
	offset int
	height int
	width  int
}

func (i *inspectionOverlay) open(spec InspectionSpec) {
	i.spec = spec
	i.offset = max(0, spec.Line-5)
	i.width = 0
	i.lines = strings.Split(i.text(), "\n")
	if spec.Items != nil {
		i.spec.Items = slices.Clone(spec.Items)
		i.picker.open(FilePickerSpec{Paths: spec.Items})
		i.picker.heading = spec.Title
		i.picker.fuzzy = true
	}
}

func (i *inspectionOverlay) text() string {
	if i.spec.Highlighted != "" {
		return i.spec.Highlighted
	}
	return strings.ReplaceAll(diff.SanitizeCommitText(i.spec.Text), "\t", "    ")
}

func (i *inspectionOverlay) render(ctx RenderCtx, mgr *Manager) string {
	i.height = max(1, ctx.Height-8)
	if i.spec.Items != nil {
		return i.picker.render(ctx, mgr)
	}
	width := max(1, min(120, ctx.Width-4))
	inner := max(1, width-4)
	if i.spec.Line == 0 && i.width != inner {
		i.lines = style.SGR{}.Reemit(strings.Split(ansi.Hardwrap(i.text(), inner, true), "\n"))
		i.width = inner
	}
	i.offset = max(0, min(i.offset, len(i.lines)-i.height))
	var rows []string
	for n := i.offset; n < min(len(i.lines), i.offset+i.height); n++ {
		text := i.lines[n]
		if i.spec.Line > 0 {
			text = fmt.Sprintf("%5d  %s", n+1, text)
		}
		text = ansi.Truncate(text, inner, "…")
		if n+1 == i.spec.Line {
			text = ctx.Resolver.Style(style.StyleKeyFileSelected).Width(inner).Render(text)
		}
		rows = append(rows, text)
	}
	box := ctx.Resolver.Style(style.StyleKeyInfoBox).Padding(1, 1).Width(width).Render(strings.Join(rows, "\n"))
	edge := borderEdgeText{accentFg: string(ctx.Resolver.Color(style.ColorKeyAccentFg)), paneBg: string(ctx.Resolver.Color(style.ColorKeyDiffPaneBg))}
	box = mgr.injectBorderTitle(box, " "+style.SanitizeFilenameForDisplay(i.spec.Title)+" ", edge)
	return mgr.injectBorderFooter(box, " ↑↓ scroll · Esc back ", edge)
}

func (i *inspectionOverlay) outcome(out Outcome) Outcome {
	if out.Kind == OutcomeFileChosen {
		return Outcome{Kind: OutcomeInspectionChosen, InspectionIndex: slices.Index(i.spec.Items, out.FileChoice.Path)}
	}
	if out.Kind == OutcomeClosed {
		return Outcome{Kind: OutcomeInspectionBack}
	}
	return out
}

func (i *inspectionOverlay) handleKey(msg tea.KeyPressMsg, action keymap.Action) Outcome {
	if msg.String() == "esc" || msg.String() == "ctrl+c" {
		return Outcome{Kind: OutcomeInspectionBack}
	}
	if i.spec.Items != nil {
		return i.outcome(i.picker.handleKey(msg, action))
	}
	switch action {
	case keymap.ActionDown:
		i.offset++
	case keymap.ActionUp:
		i.offset--
	case keymap.ActionPageDown:
		i.offset += max(1, i.height)
	case keymap.ActionPageUp:
		i.offset -= max(1, i.height)
	case keymap.ActionHalfPageDown:
		i.offset += max(1, i.height/2)
	case keymap.ActionHalfPageUp:
		i.offset -= max(1, i.height/2)
	}
	return Outcome{}
}

func (i *inspectionOverlay) handleMouse(msg tea.MouseMsg) Outcome {
	if i.spec.Items != nil {
		return i.outcome(i.picker.handleMouse(msg))
	}
	if msg, ok := msg.(tea.MouseWheelMsg); ok {
		switch msg.Button {
		case tea.MouseWheelDown:
			i.offset += WheelStep
		case tea.MouseWheelUp:
			i.offset -= WheelStep
		}
	}
	return Outcome{}
}
