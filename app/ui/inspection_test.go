package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/umputun/revdiff/app/diff"
	"github.com/umputun/revdiff/app/highlight"
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

func (s inspectionStub) Servers() []InspectionServer {
	return []InspectionServer{{Name: "example", Command: "example-server", Path: "/tools/example-server"}, {Name: "other", Command: "other-server"}}
}

func TestInspectionCommandsAndReturnToReview(t *testing.T) {
	for _, op := range []InspectionOperation{InspectHover, InspectDefinition, InspectReferences} {
		t.Run(string(op), func(t *testing.T) {
			m := testModel([]string{"a.go"}, nil)
			m.highlighter = highlight.New("monokai", true)
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
					return InspectionResult{Text: "```go\nvar π int\n```", Markdown: true, Locations: []InspectionPosition{{Path: "other.go", Line: 2, Column: 4}}}, nil
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
				require.Equal(t, "var π int", ansi.Strip(m.inspection.page.spec.Highlighted))
				require.Contains(t, m.inspection.page.spec.Highlighted, "\x1b[38;2;")
			} else {
				require.Equal(t, []string{"other.go:2:5"}, m.inspection.page.spec.Items)
				model, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
				m = model.(Model)
				model, _ = m.Update(cmd())
				m = model.(Model)
				require.Equal(t, 2, m.inspection.page.spec.Line)
				require.Equal(t, "package demo\nvar π = 3\n", m.inspection.page.spec.Text)
				require.Equal(t, m.inspection.page.spec.Text, ansi.Strip(m.inspection.page.spec.Highlighted))
				require.Contains(t, m.inspection.page.spec.Highlighted, "\x1b[38;2;")
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

func TestLSPListAndCommandFromPopup(t *testing.T) {
	m := testModel(nil, nil)
	m.inspection.provider = inspectionStub{}
	m.startCommand()
	m.command.input.SetValue("lsp list")
	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(Model)
	model, _ = m.Update(cmd())
	m = model.(Model)
	require.Contains(t, m.inspection.page.spec.Text, "example — example-server (/tools/example-server)")
	require.Contains(t, m.inspection.page.spec.Text, "other — other-server (not on PATH)")
	require.Contains(t, m.inspection.page.spec.Text, ":lsp install other")
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	m = model.(Model)
	require.True(t, m.command.active)
	require.False(t, m.overlay.Active())
}

func TestInspectionSyntaxHighlighting(t *testing.T) {
	m := Model{highlighter: highlight.New("monokai", true)}
	for _, tc := range []struct{ language, code string }{
		{"go", "func twice(n int) int { return n * 2 }"},
		{"typescript", "const answer: number = 42;"},
		{"python", "def twice(n):\n    return n * 2"},
		{"rust", "fn twice(n: i32) -> i32 { n * 2 }"},
	} {
		t.Run(tc.language, func(t *testing.T) {
			text := "Documentation\n\n~~~" + tc.language + "\n" + tc.code + "\n~~~\nMore docs."
			got := m.inspectionMarkdown("unrelated.txt", text)
			require.Equal(t, "Documentation\n\n"+tc.code+"\nMore docs.", ansi.Strip(got))
			require.Contains(t, got, "\x1b[38;2;249;38;114m", "Monokai keyword color, not Markdown's code-block color")
			m.highlighter.SetStyle("github")
			require.NotEqual(t, got, m.inspectionMarkdown("unrelated.txt", text), "the active theme controls token colors")
			m.highlighter.SetStyle("monokai")
		})
	}
	text := "Before\n````go\nvar x = `\n```\n`\n````\nAfter\n```unknown-language\nliteral\n```"
	require.Equal(t, "Before\nvar x = `\n```\n`\nAfter\nliteral", ansi.Strip(m.inspectionMarkdown("a.go", text)))
	got := m.inspectionMarkdown("a.go", "```\n\x1b[31mvar x = 1\x1b]52;c;bad\a\n```")
	require.Equal(t, "var x = 1]52;c;bad", ansi.Strip(got), "the sanitizer neutralizes OSC controls, leaving harmless text")
	require.NotContains(t, got, "\x1b]52")
	require.NotContains(t, got, "\x1b[31m")
	require.Contains(t, got, "\x1b[38;2;")
	m.highlighter = highlight.New("monokai", false)
	require.Equal(t, "var x = 1", m.inspectionMarkdown("a.go", "```go\nvar x = 1\n```"))
	require.Equal(t, "a\n\n    b\n", m.inspectionCode("unknown-language", "a\n\n\tb\n"))
	require.Equal(t, 4, len(strings.Split(m.inspectionCode("a.go", "a\n\n\tb\n"), "\n")))
}
