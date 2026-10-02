package ui

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/umputun/revdiff/app/diff"
	"github.com/umputun/revdiff/app/ui/overlay"
)

type inspectionStub struct {
	query func(context.Context, InspectionOperation, InspectionPosition, string) (InspectionResult, error)
	read  func(context.Context, string) (string, error)
}

func (s inspectionStub) Query(ctx context.Context, op InspectionOperation, pos InspectionPosition, line string) (InspectionResult, error) {
	return s.query(ctx, op, pos, line)
}

func (s inspectionStub) ReadSource(ctx context.Context, path string) (string, error) {
	return s.read(ctx, path)
}

func TestInspectionCommandsAndReturnToReview(t *testing.T) {
	for _, op := range []InspectionOperation{InspectHover, InspectDefinition, InspectReferences} {
		t.Run(string(op), func(t *testing.T) {
			m := testModel([]string{"a.go"}, nil)
			m.filesLoaded = true
			m.layout.focus = paneDiff
			m.file.name = "a.go"
			// Repeat the identifier after a multibyte prefix to catch byte/rune confusion.
			m.file.lines = []diff.DiffLine{{Content: "header"}, {NewNum: 8, Content: "π := π + π", ChangeType: diff.ChangeAdd}}
			m.nav.diffCursor, m.layout.viewport.YOffset = 1, 1
			m.inspection.provider = inspectionStub{
				query: func(_ context.Context, got InspectionOperation, pos InspectionPosition, line string) (InspectionResult, error) {
					require.Equal(t, op, got)
					require.Equal(t, InspectionPosition{Path: "a.go", Line: 8, Column: 11}, pos)
					require.Equal(t, "π := π + π", line)
					return InspectionResult{Text: "var π int", Locations: []InspectionPosition{{Path: "other.go", Line: 2, Column: 4}}}, nil
				},
				read: func(_ context.Context, path string) (string, error) {
					require.Equal(t, "other.go", path)
					return "package demo\nvar π = 3\n", nil
				},
			}
			m.startCommand()
			m.command.input.SetValue("inspect " + string(op))
			model, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			m = model.(Model)
			require.Equal(t, overlay.KindInspection, m.overlay.Kind())
			require.Equal(t, []string{"π · column 1", "π · column 6", "π · column 10"}, m.inspection.page.spec.Items)
			for range 2 {
				model, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
				m = model.(Model)
			}
			model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			m = model.(Model)
			require.NotNil(t, cmd)
			model, _ = m.Update(cmd())
			m = model.(Model)
			backs := 2
			if op == InspectHover {
				require.Equal(t, "var π int", m.inspection.page.spec.Text)
			} else {
				require.Equal(t, []string{"other.go:2:5"}, m.inspection.page.spec.Items)
				model, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
				m = model.(Model)
				model, _ = m.Update(cmd())
				m = model.(Model)
				require.Equal(t, 2, m.inspection.page.spec.Line)
				require.Equal(t, "package demo\nvar π = 3\n", m.inspection.page.spec.Text)
				backs++
			}
			for range backs {
				model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
				m = model.(Model)
			}
			require.False(t, m.overlay.Active())
			require.Equal(t, "a.go", m.file.name)
			require.Equal(t, 1, m.nav.diffCursor)
			require.Equal(t, 1, m.layout.viewport.YOffset)
			require.Equal(t, paneDiff, m.layout.focus)
		})
	}
}

func TestInspectionCancellationAndStaleReply(t *testing.T) {
	m := testModel([]string{"a.go"}, nil)
	m.filesLoaded, m.layout.focus, m.file.name = true, paneDiff, "a.go"
	m.file.lines = []diff.DiffLine{{NewNum: 1, Content: "symbol", ChangeType: diff.ChangeContext}}
	m.inspection.provider = inspectionStub{query: func(ctx context.Context, _ InspectionOperation, _ InspectionPosition, _ string) (InspectionResult, error) {
		require.ErrorIs(t, ctx.Err(), context.Canceled)
		return InspectionResult{}, errors.New("late reply")
	}}
	model, _ := m.openInspection(InspectHover)
	m = model.(Model)
	model, cmd := m.chooseInspection(0)
	m = model.(Model)
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = model.(Model)
	model, _ = m.Update(cmd())
	m = model.(Model)
	require.Equal(t, inspectionSymbols, m.inspection.page.kind)
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	require.False(t, model.(Model).overlay.Active())
}

func TestInspectionGuards(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*Model)
		hint  string
	}{
		{"historical", func(m *Model) { m.inspection.provider = nil }, "working-tree"},
		{"tree", func(m *Model) { m.layout.focus = paneTree }, "Focus the diff"},
		{"staged", func(m *Model) { m.file.staged = true }, "staged"},
		{"loading", func(m *Model) { m.file.requestedPath = "b.go" }, "Wait"},
		{"deleted", func(m *Model) { m.file.lines[0].ChangeType = diff.ChangeRemove }, "current source line"},
		{"annotation", func(m *Model) { m.annot.cursorOnAnnotation = true }, "current source line"},
		{"empty", func(m *Model) { m.file.lines[0].Content = "{}" }, "No identifiers"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := testModel([]string{"a.go"}, nil)
			m.filesLoaded, m.layout.focus, m.file.name = true, paneDiff, "a.go"
			m.file.lines = []diff.DiffLine{{NewNum: 1, Content: "symbol", ChangeType: diff.ChangeContext}}
			m.inspection.provider = inspectionStub{}
			tc.setup(&m)
			model, cmd := m.openInspection(InspectHover)
			require.Nil(t, cmd)
			require.False(t, model.(Model).overlay.Active())
			require.Contains(t, model.(Model).keys.hint, tc.hint)
		})
	}
}

func TestLSPInstallUsesShellRegistry(t *testing.T) {
	m := testModel(nil, nil)
	m.inspection.installCommands = map[string]string{"example": "package-tool install language-server"}
	runner := &shellStub{}
	m.shell = runner
	m.startCommand()
	m.command.input.SetValue("lsp install ex")
	require.Equal(t, "lsp install example", m.commandMatches()[0].name)
	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	require.Equal(t, "package-tool install language-server", runner.command)
	require.False(t, model.(Model).command.active)
}
