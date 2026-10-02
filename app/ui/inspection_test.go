package ui

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/umputun/revdiff/app/diff"
	"github.com/umputun/revdiff/app/highlight"
	"github.com/umputun/revdiff/app/ui/overlay"
)

type inspectionStub struct {
	symbols func(context.Context, InspectionPosition, string) ([]InspectionSymbol, error)
	query   func(context.Context, InspectionOperation, InspectionPosition, string) (InspectionResult, error)
	read    func(context.Context, string) (string, error)
}

func (s inspectionStub) Symbols(ctx context.Context, pos InspectionPosition, line string) ([]InspectionSymbol, error) {
	if s.symbols != nil {
		return s.symbols(ctx, pos, line)
	}
	return []InspectionSymbol{{Name: "symbol", Column: 0}}, nil
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

type inspectionKeyProbe struct{ Model }

func (m inspectionKeyProbe) Init() tea.Cmd { return nil }
func (m inspectionKeyProbe) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	model, cmd := m.Model.Update(msg)
	m.Model = model.(Model)
	if _, ok := msg.(inspectionLoadedMsg); ok {
		return m, tea.Quit
	}
	return m, cmd
}

func TestInspectShortcutKeepsDiffFocus(t *testing.T) {
	m := testModel([]string{"a.go"}, nil)
	m.filesLoaded, m.layout.focus, m.file.name = true, paneDiff, "a.go"
	m.file.lines = []diff.DiffLine{{NewNum: 1, Content: "type Name struct{}", ChangeType: diff.ChangeContext}}
	m.inspection.provider = inspectionStub{symbols: func(_ context.Context, _ InspectionPosition, line string) ([]InspectionSymbol, error) {
		require.Equal(t, "type Name struct{}", line)
		return []InspectionSymbol{{"Name", 5}}, nil
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	p := tea.NewProgram(inspectionKeyProbe{m}, tea.WithContext(ctx), tea.WithInput(strings.NewReader("\x1b[13;9u")), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler())
	model, err := p.Run()
	require.NoError(t, err)
	m = model.(inspectionKeyProbe).Model
	require.False(t, m.command.active)
	require.False(t, m.annot.annotating)
	require.Equal(t, paneDiff, m.layout.focus)
	require.Equal(t, []string{"Name · column 6"}, m.inspection.page.spec.Items)
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
			m.nav.diffCursor = 1
			m.layout.viewport.SetContent("header\nπ := π + π")
			m.layout.viewport.SetYOffset(1)
			m.inspection.provider = inspectionStub{
				symbols: func(_ context.Context, pos InspectionPosition, line string) ([]InspectionSymbol, error) {
					require.Equal(t, 8, pos.Line)
					require.Equal(t, "π := π + π", line)
					return []InspectionSymbol{{"π", 0}, {"π", 6}, {"π", 11}}, nil
				},
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
			verb := string(op)
			if op == InspectHover {
				verb = "inspect"
			}
			m.command.input.SetValue("lsp symbol " + verb)
			model, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = model.(Model)
			model, _ = m.Update(cmd())
			m = model.(Model)
			require.Equal(t, overlay.KindInspection, m.overlay.Kind())
			require.Equal(t, []string{"π · column 1", "π · column 6", "π · column 10"}, m.inspection.page.spec.Items)
			for range 2 {
				model, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
				m = model.(Model)
			}
			model, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
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
				model, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
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
				model, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
				m = model.(Model)
			}
			require.False(t, m.overlay.Active())
			require.Equal(t, "a.go", m.file.name)
			require.Equal(t, 1, m.nav.diffCursor)
			require.Equal(t, 1, m.layout.viewport.YOffset())
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
	model, cmd := m.openInspection(InspectHover)
	m = model.(Model)
	model, _ = m.Update(cmd())
	m = model.(Model)
	model, cmd = m.chooseInspection(0)
	m = model.(Model)
	model, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = model.(Model)
	model, _ = m.Update(cmd())
	m = model.(Model)
	require.Equal(t, inspectionSymbols, m.inspection.page.kind)
	model, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
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

func TestLSPInstallCommand(t *testing.T) {
	for _, state := range []string{"ready", "unavailable", "busy"} {
		t.Run(state, func(t *testing.T) {
			m := testModel(nil, nil)
			m.inspection.installCommands = map[string]string{"example": "package-tool install language-server"}
			runner := &shellStub{}
			if state != "unavailable" {
				m.shell = runner
			}
			if state == "busy" {
				m.live.operation = liveSending
			}
			m.startCommand()
			m.command.input.SetValue("lsp install ex")
			require.Equal(t, "lsp install example", m.commandMatches()[0].name)
			model, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = model.(Model)
			if state != "ready" {
				require.Nil(t, cmd)
				require.True(t, m.command.active, "keep errors visible in the palette")
				require.NotEmpty(t, m.command.err)
				require.Empty(t, runner.command)
				require.Empty(t, m.command.history)
				return
			}
			require.NotNil(t, cmd)
			require.Equal(t, "package-tool install language-server", runner.command)
			require.False(t, m.command.active)
			require.Equal(t, []string{"lsp install example"}, m.command.history)
		})
	}
}

func TestLSPListAndCommandFromPopup(t *testing.T) {
	m := testModel(nil, nil)
	m.inspection.provider = inspectionStub{}
	m.startCommand()
	m.command.input.SetValue("lsp list")
	model, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = model.(Model)
	model, _ = m.Update(cmd())
	m = model.(Model)
	require.Contains(t, m.inspection.page.spec.Text, "example — example-server (/tools/example-server)")
	require.Contains(t, m.inspection.page.spec.Text, "other — other-server (not on PATH)")
	require.Contains(t, m.inspection.page.spec.Text, ":lsp install other")
	model, _ = m.Update(tea.KeyPressMsg{Text: ":"})
	m = model.(Model)
	require.True(t, m.command.active)
	require.False(t, m.overlay.Active())
}

func TestFileSymbolsFuzzySelectAndPreview(t *testing.T) {
	for _, focus := range []pane{paneTree, paneDiff} {
		m := testModel([]string{"active.go"}, nil)
		m.filesLoaded, m.layout.focus, m.file.name = true, focus, "active.go"
		m.inspection.provider = inspectionStub{
			query: func(_ context.Context, op InspectionOperation, pos InspectionPosition, _ string) (InspectionResult, error) {
				require.Equal(t, InspectSymbols, op)
				require.Equal(t, "active.go", pos.Path)
				return InspectionResult{Symbols: []InspectionDocumentSymbol{
					{"Other", InspectionPosition{"active.go", 2, 0}},
					{"Parser.ParseRequest", InspectionPosition{"active.go", 19, 5}},
				}}, nil
			},
			read: func(_ context.Context, path string) (string, error) {
				require.Equal(t, "active.go", path)
				return strings.Repeat("// context\n", 18) + "func ParseRequest() {}", nil
			},
		}
		m.startCommand()
		m.command.input.SetValue("lsp symbol list")
		model, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = model.(Model)
		model, _ = m.Update(cmd())
		m = model.(Model)
		model, _ = m.Update(tea.KeyPressMsg{Text: "pprq"})
		m = model.(Model)
		model, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = model.(Model)
		require.NotNil(t, cmd, "non-contiguous fuzzy query selects the declaration")
		model, _ = m.Update(cmd())
		m = model.(Model)
		require.Equal(t, 19, m.inspection.page.spec.Line)
		require.Equal(t, focus, m.layout.focus)
		require.Equal(t, "active.go", m.file.name)
	}
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
