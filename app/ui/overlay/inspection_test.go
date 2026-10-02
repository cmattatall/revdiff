package overlay

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/umputun/revdiff/app/keymap"
	"github.com/umputun/revdiff/app/ui/style"
)

func TestInspectionFilteredSelection(t *testing.T) {
	m := NewManager()
	m.OpenInspection(InspectionSpec{Title: "Inspect references", Items: []string{"first.go:7:4", "second.go:12:8", "third.go:4:1"}})
	m.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("second")}, "")
	out := m.HandleKey(tea.KeyMsg{Type: tea.KeyEnter}, keymap.ActionConfirm)
	require.Equal(t, OutcomeInspectionChosen, out.Kind)
	require.Equal(t, 1, out.InspectionIndex, "selection uses the original list, not the filtered index")
	require.True(t, m.Active())
	out = m.HandleKey(tea.KeyMsg{Type: tea.KeyEsc}, "")
	require.Equal(t, OutcomeInspectionBack, out.Kind)
}

func TestInspectionSourceAndResize(t *testing.T) {
	m := NewManager()
	var lines []string
	for n := 1; n <= 80; n++ {
		lines = append(lines, fmt.Sprintf("source line %d", n))
	}
	m.OpenInspection(InspectionSpec{Title: "definition", Text: strings.Join(lines, "\n"), Line: 40})
	ctx := RenderCtx{Width: 80, Height: 22, Resolver: style.PlainResolver()}
	view := m.inspect.render(ctx, m)
	require.Contains(t, view, "   40  source line 40")
	require.LessOrEqual(t, lipgloss.Height(view), ctx.Height)
	m.HandleKey(tea.KeyMsg{Type: tea.KeyPgDown}, keymap.ActionPageDown)
	view = m.inspect.render(ctx, m)
	require.Contains(t, view, "   51  source line 51")
	ctx.Width, ctx.Height = 34, 14
	view = m.inspect.render(ctx, m)
	require.LessOrEqual(t, lipgloss.Width(view), ctx.Width)
	require.LessOrEqual(t, lipgloss.Height(view), ctx.Height)
	m.HandleMouse(tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	view = m.inspect.render(ctx, m)
	require.Contains(t, view, "source line 47")
}

func TestInspectionHoverWrapAndSanitization(t *testing.T) {
	m := NewManager()
	text := strings.Repeat("documentation ", 12) + "THE_END"
	m.OpenInspection(InspectionSpec{Title: "hover", Text: "\x1b]52;c;clipboard\a" + text})
	ctx := RenderCtx{Width: 34, Height: 30, Resolver: style.PlainResolver()}
	view := m.inspect.render(ctx, m)
	require.Contains(t, ansi.Strip(view), "THE_END", "long hover paragraphs wrap instead of losing the tail")
	require.NotContains(t, view, "\x1b]52")
	require.LessOrEqual(t, lipgloss.Width(view), ctx.Width)
	ctx.Width = 90
	_ = m.inspect.render(ctx, m)
	require.Less(t, len(m.inspect.lines), 5, "resizing reflows the text")
}
