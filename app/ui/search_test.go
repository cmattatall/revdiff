package ui

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/revdiff/app/diff"
	"github.com/umputun/revdiff/app/ui/mocks"
	"github.com/umputun/revdiff/app/ui/style"
)

func TestModel_HighlightSearchMatches(t *testing.T) {
	colors := style.Colors{SearchFg: "#1a1a1a", SearchBg: "#d7d700"}
	for _, tt := range []struct {
		name, input, term string
		start, end        int
		plain             bool
	}{
		{"plain", "say hello world", "hello", 4, 9, false},
		{"syntax reset inside match", "say \033[32mhe\033[0mllo\033[31m world", "hello", 4, 9, false},
		{"word background inside match", "say he\033[48;2;45;90;58mllo world", "hello", 4, 9, false},
		{"word reverse", "say \033[7mhello world", "hello", 4, 9, true},
		{"case insensitive", "say HELLO world", "hello", 4, 9, false},
		{"unicode case changes byte length", "K say hello world", "hello", 6, 11, false},
		{"no match", "say hello world", "absent", 0, 0, false},
		{"empty query", "say hello world", "", 0, 0, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := testModel(nil, nil)
			if !tt.plain {
				m.resolver = style.NewResolver(colors)
			}
			m.search.term = tt.term
			got := m.highlightSearchMatches(tt.input, lipgloss.NewStyle())
			require.Equal(t, ansi.Strip(tt.input), ansi.Strip(got), "highlighting must preserve all text")
			before := uv.NewScreenBuffer(40, 1)
			after := uv.NewScreenBuffer(40, 1)
			uv.NewStyledString(tt.input).Draw(&before, before.Bounds())
			uv.NewStyledString(got).Draw(&after, after.Bounds())
			for x := range ansi.StringWidth(tt.input) {
				cell := after.CellAt(x, 0)
				if x < tt.start || x >= tt.end {
					assert.True(t, cell.Equal(before.CellAt(x, 0)), "unchanged style at column %d", x)
					continue
				}
				assert.NotZero(t, cell.Style.Attrs&uv.AttrBold)
				if tt.plain {
					assert.NotZero(t, cell.Style.Attrs&uv.AttrReverse)
				} else {
					assert.Equal(t, color.NRGBA{26, 26, 26, 255}, color.NRGBAModel.Convert(cell.Style.Fg))
					assert.Equal(t, color.NRGBA{215, 215, 0, 255}, color.NRGBAModel.Convert(cell.Style.Bg))
					assert.Zero(t, cell.Style.Attrs&(uv.AttrFaint|uv.AttrReverse))
				}
			}
		})
	}
}

func TestModel_SearchHighlightOverridesDiffStyles(t *testing.T) {
	for _, mode := range []string{"context", "add", "remove", "collapsed add", "collapsed modify"} {
		for _, wrap := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/wrap=%v", mode, wrap), func(t *testing.T) {
				m := testModel(nil, nil)
				res := style.NewResolver(style.Colors{
					SearchFg: "#1a1a1a", SearchBg: "#ffd700", Muted: "#999999",
					AddFg: "#87d787", AddBg: "#123800", RemoveFg: "#ff8787", RemoveBg: "#4d1100",
					ModifyFg: "#f5c542", ModifyBg: "#3d2e00",
				})
				m.resolver = res
				m.renderer = style.NewRenderer(res)
				m.sgr = style.SGR{}
				m.layout.width, m.layout.treeWidth = 80, 0
				m.modes.wrap = wrap
				if wrap {
					m.layout.width = 24
				}
				change := diff.ChangeContext
				if strings.Contains(mode, "add") || mode == "collapsed modify" {
					change = diff.ChangeAdd
				} else if mode == "remove" {
					change = diff.ChangeRemove
				}
				m.modes.collapsed.enabled = strings.HasPrefix(mode, "collapsed")
				m.file.lines = []diff.DiffLine{{Content: "prefix needle middle needle suffix", ChangeType: change}}
				m.file.highlighted = []string{"\033[32mprefix ne\033[31medle middle needle suffix\033[0m"}
				if mode == "collapsed modify" {
					m.file.lines = append([]diff.DiffLine{{Content: "old", ChangeType: diff.ChangeRemove}}, m.file.lines...)
					m.file.highlighted = append([]string{"old"}, m.file.highlighted...)
				}
				baseline := m.renderDiff()
				m.search.term = "needle middle"
				m.search.matches = []int{len(m.file.lines) - 1}
				got := m.renderDiff()
				require.Equal(t, ansi.Strip(baseline), ansi.Strip(got))
				before, after := uv.NewScreenBuffer(80, 20), uv.NewScreenBuffer(80, 20)
				uv.NewStyledString(baseline).Draw(&before, before.Bounds())
				uv.NewStyledString(got).Draw(&after, after.Bounds())
				var highlighted strings.Builder
				for y := range 20 {
					for x := range 80 {
						cell := after.CellAt(x, y)
						if cell != nil && cell.Style.Bg != nil && color.NRGBAModel.Convert(cell.Style.Bg) == (color.NRGBA{255, 215, 0, 255}) {
							highlighted.WriteString(strings.TrimSpace(cell.Content))
							assert.Equal(t, color.NRGBA{26, 26, 26, 255}, color.NRGBAModel.Convert(cell.Style.Fg))
							assert.NotZero(t, cell.Style.Attrs&uv.AttrBold)
						} else {
							want := before.CellAt(x, y)
							if cell != nil && want != nil && cell.Content == " " && want.Content == " " {
								// Foreground is invisible on blank gutter cells.
								assert.Equal(t, want.Style.Bg, cell.Style.Bg)
								assert.Equal(t, want.Style.Attrs, cell.Style.Attrs)
							} else {
								assert.Equal(t, want, cell, "nonmatch at %d,%d retains its style", x, y)
							}
						}
					}
				}
				require.Equal(t, "needlemiddle", highlighted.String(), "the entire match stays visible across wrapping and syntax styles")
			})
		}
	}
}

func TestModel_StartSearch(t *testing.T) {
	lines := []diff.DiffLine{
		{NewNum: 1, Content: "line1", ChangeType: diff.ChangeContext},
		{NewNum: 2, Content: "added", ChangeType: diff.ChangeAdd},
	}
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := result.(Model)
	result, _ = model.Update(fileLoadedMsg{file: "a.go", lines: lines})
	model = result.(Model)
	model.layout.focus = paneDiff

	// press / to start search
	result, _ = model.Update(tea.KeyPressMsg{Code: '/', Text: string('/')})
	model = result.(Model)

	assert.True(t, model.search.active, "should be in searching mode")
	assert.True(t, model.search.input.Focused(), "search input should be focused")
}

func TestModel_SearchStandardTextEditing(t *testing.T) {
	for _, tt := range []struct {
		name string
		key  tea.KeyPressMsg
		want string
		pos  int
	}{
		{"home", tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}, "αβ γδ", 0},
		{"end", tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl}, "αβ γδ", 5},
		{"back", tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl}, "αβ γδ", 2},
		{"forward", tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl}, "αβ γδ", 4},
		{"delete word before cursor", tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl}, "γδ", 0},
		{"delete before cursor", tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}, "γδ", 0},
		{"delete after cursor", tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}, "αβ ", 3},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := testModel(nil, nil)
			m.search.active = true
			m.search.input = textinput.New()
			m.search.input.Focus()
			m.search.input.SetValue("αβ γδ")
			m.search.input.SetCursor(3)

			result, _ := m.Update(tt.key)
			model := result.(Model)
			assert.Equal(t, tt.want, model.search.input.Value())
			assert.Equal(t, tt.pos, model.search.input.Position())
		})
	}
}

func TestModel_StartSearchFromTreePane(t *testing.T) {
	lines := []diff.DiffLine{{NewNum: 1, Content: "line1", ChangeType: diff.ChangeContext}}
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := result.(Model)
	result, _ = model.Update(fileLoadedMsg{file: "a.go", lines: lines})
	model = result.(Model)
	model.layout.focus = paneTree

	// Tree search uses the same palette with its scope identified.
	result, _ = model.Update(tea.KeyPressMsg{Code: '/', Text: string('/')})
	model = result.(Model)
	assert.True(t, model.search.active)
	assert.Contains(t, ansi.Strip(model.commandPaneView()), "Search file tree")
}

func TestModel_SubmitSearchFindsMatches(t *testing.T) {
	lines := []diff.DiffLine{
		{NewNum: 1, Content: "hello world", ChangeType: diff.ChangeContext},
		{NewNum: 2, Content: "foo bar", ChangeType: diff.ChangeAdd},
		{NewNum: 3, Content: "hello again", ChangeType: diff.ChangeContext},
	}
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := result.(Model)
	result, _ = model.Update(fileLoadedMsg{file: "a.go", lines: lines})
	model = result.(Model)
	model.layout.focus = paneDiff
	model.nav.diffCursor = 0

	// start search
	result, _ = model.Update(tea.KeyPressMsg{Code: '/', Text: string('/')})
	model = result.(Model)

	// type "hello"
	for _, ch := range "hello" {
		result, _ = model.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		model = result.(Model)
	}

	// submit with enter
	result, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = result.(Model)

	assert.False(t, model.search.active, "should exit searching mode")
	assert.Equal(t, "hello", model.search.term)
	assert.Equal(t, []int{0, 2}, model.search.matches)
	assert.Equal(t, 0, model.search.cursor)
	assert.Equal(t, 0, model.nav.diffCursor, "cursor should be on first match")
}

func TestModel_SubmitSearchCaseInsensitive(t *testing.T) {
	lines := []diff.DiffLine{
		{NewNum: 1, Content: "Hello World", ChangeType: diff.ChangeContext},
		{NewNum: 2, Content: "HELLO again", ChangeType: diff.ChangeAdd},
	}
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := result.(Model)
	result, _ = model.Update(fileLoadedMsg{file: "a.go", lines: lines})
	model = result.(Model)
	model.layout.focus = paneDiff

	model.search.active = true
	model.search.input = textinput.New()
	model.search.input.SetValue("hello")

	result, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = result.(Model)

	assert.Equal(t, []int{0, 1}, model.search.matches, "should match case-insensitively")
}

func TestModel_SubmitSearchJumpsForwardFromCursor(t *testing.T) {
	lines := []diff.DiffLine{
		{NewNum: 1, Content: "match here", ChangeType: diff.ChangeContext},
		{NewNum: 2, Content: "foo bar", ChangeType: diff.ChangeAdd},
		{NewNum: 3, Content: "match again", ChangeType: diff.ChangeContext},
	}
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := result.(Model)
	result, _ = model.Update(fileLoadedMsg{file: "a.go", lines: lines})
	model = result.(Model)
	model.layout.focus = paneDiff
	model.nav.diffCursor = 1 // cursor past first match

	model.search.active = true
	model.search.input = textinput.New()
	model.search.input.SetValue("match")

	result, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = result.(Model)

	assert.Equal(t, 1, model.search.cursor, "should jump to second match (index 1)")
	assert.Equal(t, 2, model.nav.diffCursor, "cursor should be on second match line")
}

func TestModel_SubmitSearchWrapsToFirstMatch(t *testing.T) {
	lines := []diff.DiffLine{
		{NewNum: 1, Content: "match here", ChangeType: diff.ChangeContext},
		{NewNum: 2, Content: "foo bar", ChangeType: diff.ChangeAdd},
	}
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := result.(Model)
	result, _ = model.Update(fileLoadedMsg{file: "a.go", lines: lines})
	model = result.(Model)
	model.layout.focus = paneDiff
	model.nav.diffCursor = 1 // cursor past all matches

	model.search.active = true
	model.search.input = textinput.New()
	model.search.input.SetValue("match")

	result, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = result.(Model)

	assert.Equal(t, 0, model.search.cursor, "should wrap to first match")
	assert.Equal(t, 0, model.nav.diffCursor, "cursor should be on first match line")
}

func TestModel_SubmitSearchNoMatches(t *testing.T) {
	lines := []diff.DiffLine{
		{NewNum: 1, Content: "hello", ChangeType: diff.ChangeContext},
	}
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := result.(Model)
	result, _ = model.Update(fileLoadedMsg{file: "a.go", lines: lines})
	model = result.(Model)
	model.layout.focus = paneDiff

	model.search.active = true
	model.search.input = textinput.New()
	model.search.input.SetValue("xyz")

	result, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = result.(Model)

	assert.False(t, model.search.active)
	assert.Equal(t, "xyz", model.search.term)
	assert.Empty(t, model.search.matches)
}

func TestModel_SubmitEmptySearchClearsMatches(t *testing.T) {
	lines := []diff.DiffLine{
		{NewNum: 1, Content: "hello", ChangeType: diff.ChangeContext},
	}
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := result.(Model)
	result, _ = model.Update(fileLoadedMsg{file: "a.go", lines: lines})
	model = result.(Model)
	model.layout.focus = paneDiff

	// set up existing search state
	model.search.term = "hello"
	model.search.matches = []int{0}
	model.search.cursor = 0

	// start search with empty input
	model.search.active = true
	model.search.input = textinput.New()
	model.search.input.SetValue("")

	result, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = result.(Model)

	assert.False(t, model.search.active)
	assert.Empty(t, model.search.term)
	assert.Empty(t, model.search.matches)
}

func TestModel_CancelSearch(t *testing.T) {
	lines := []diff.DiffLine{
		{NewNum: 1, Content: "hello", ChangeType: diff.ChangeContext},
	}
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := result.(Model)
	result, _ = model.Update(fileLoadedMsg{file: "a.go", lines: lines})
	model = result.(Model)
	model.layout.focus = paneDiff

	// start search
	result, _ = model.Update(tea.KeyPressMsg{Code: '/', Text: string('/')})
	model = result.(Model)
	require.True(t, model.search.active)

	// cancel with esc
	result, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model = result.(Model)

	assert.False(t, model.search.active, "should exit searching mode on esc")
}

func TestModel_CancelSearchPreservesExistingMatches(t *testing.T) {
	lines := []diff.DiffLine{
		{NewNum: 1, Content: "hello", ChangeType: diff.ChangeContext},
	}
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := result.(Model)
	result, _ = model.Update(fileLoadedMsg{file: "a.go", lines: lines})
	model = result.(Model)
	model.layout.focus = paneDiff

	// set up existing search state
	model.search.term = "hello"
	model.search.matches = []int{0}

	// start and cancel new search
	result, _ = model.Update(tea.KeyPressMsg{Code: '/', Text: string('/')})
	model = result.(Model)
	result, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model = result.(Model)

	assert.Equal(t, "hello", model.search.term, "existing search term should be preserved on cancel")
	assert.Equal(t, []int{0}, model.search.matches, "existing matches should be preserved on cancel")
}

func TestModel_SearchInputForwardsCharacters(t *testing.T) {
	lines := []diff.DiffLine{{NewNum: 1, Content: "hello", ChangeType: diff.ChangeContext}}
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := result.(Model)
	result, _ = model.Update(fileLoadedMsg{file: "a.go", lines: lines})
	model = result.(Model)
	model.layout.focus = paneDiff

	// start search
	result, _ = model.Update(tea.KeyPressMsg{Code: '/', Text: string('/')})
	model = result.(Model)

	// type characters
	for _, ch := range "test" {
		result, _ = model.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		model = result.(Model)
	}

	assert.Equal(t, "test", model.search.input.Value(), "characters should be forwarded to search input")
}

func TestModel_SearchBlocksOtherKeysWhileActive(t *testing.T) {
	lines := []diff.DiffLine{{NewNum: 1, Content: "hello", ChangeType: diff.ChangeContext}}
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := result.(Model)
	result, _ = model.Update(fileLoadedMsg{file: "a.go", lines: lines})
	model = result.(Model)
	model.layout.focus = paneDiff

	// start search
	result, _ = model.Update(tea.KeyPressMsg{Code: '/', Text: string('/')})
	model = result.(Model)

	// pressing q should not quit, it should type 'q'
	result, _ = model.Update(tea.KeyPressMsg{Code: 'q', Text: string('q')})
	model = result.(Model)

	assert.True(t, model.search.active, "should still be searching")
	assert.Contains(t, model.search.input.Value(), "q")
}

func TestModel_SearchForwardsNonKeyMessages(t *testing.T) {
	lines := []diff.DiffLine{{NewNum: 1, Content: "hello", ChangeType: diff.ChangeContext}}
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := result.(Model)
	result, _ = model.Update(fileLoadedMsg{file: "a.go", lines: lines})
	model = result.(Model)
	model.layout.focus = paneDiff

	// start search
	result, _ = model.Update(tea.KeyPressMsg{Code: '/', Text: string('/')})
	model = result.(Model)
	require.True(t, model.search.active)

	// send a non-key message; should not panic and model stays searching
	type customMsg struct{}
	result, _ = model.Update(customMsg{})
	model = result.(Model)
	assert.True(t, model.search.active, "searching should remain true after non-key message")
}

func TestModel_NextSearchMatch(t *testing.T) {
	lines := []diff.DiffLine{
		{NewNum: 1, Content: "match one", ChangeType: diff.ChangeContext},
		{NewNum: 2, Content: "no match", ChangeType: diff.ChangeAdd},
		{NewNum: 3, Content: "match two", ChangeType: diff.ChangeContext},
		{NewNum: 4, Content: "match three", ChangeType: diff.ChangeAdd},
	}
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := result.(Model)
	result, _ = model.Update(fileLoadedMsg{file: "a.go", lines: lines})
	model = result.(Model)
	model.layout.focus = paneDiff
	model.search.matches = []int{0, 2, 3}
	model.search.cursor = 0
	model.nav.diffCursor = 0

	// press n to go to next match
	result, _ = model.Update(tea.KeyPressMsg{Code: 'n', Text: string('n')})
	model = result.(Model)
	assert.Equal(t, 1, model.search.cursor, "search cursor should advance to 1")
	assert.Equal(t, 2, model.nav.diffCursor, "diff cursor should move to second match")

	// press n again
	result, _ = model.Update(tea.KeyPressMsg{Code: 'n', Text: string('n')})
	model = result.(Model)
	assert.Equal(t, 2, model.search.cursor, "search cursor should advance to 2")
	assert.Equal(t, 3, model.nav.diffCursor, "diff cursor should move to third match")
}

func TestModel_NextSearchMatchWrapsAround(t *testing.T) {
	lines := []diff.DiffLine{
		{NewNum: 1, Content: "match one", ChangeType: diff.ChangeContext},
		{NewNum: 2, Content: "no match", ChangeType: diff.ChangeAdd},
		{NewNum: 3, Content: "match two", ChangeType: diff.ChangeContext},
	}
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := result.(Model)
	result, _ = model.Update(fileLoadedMsg{file: "a.go", lines: lines})
	model = result.(Model)
	model.layout.focus = paneDiff
	model.search.matches = []int{0, 2}
	model.search.cursor = 1 // on last match
	model.nav.diffCursor = 2

	// press n should wrap to first match
	result, _ = model.Update(tea.KeyPressMsg{Code: 'n', Text: string('n')})
	model = result.(Model)
	assert.Equal(t, 0, model.search.cursor, "search cursor should wrap to 0")
	assert.Equal(t, 0, model.nav.diffCursor, "diff cursor should wrap to first match")
}

func TestModel_PrevSearchMatch(t *testing.T) {
	lines := []diff.DiffLine{
		{NewNum: 1, Content: "match one", ChangeType: diff.ChangeContext},
		{NewNum: 2, Content: "no match", ChangeType: diff.ChangeAdd},
		{NewNum: 3, Content: "match two", ChangeType: diff.ChangeContext},
		{NewNum: 4, Content: "match three", ChangeType: diff.ChangeAdd},
	}
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := result.(Model)
	result, _ = model.Update(fileLoadedMsg{file: "a.go", lines: lines})
	model = result.(Model)
	model.layout.focus = paneDiff
	model.search.matches = []int{0, 2, 3}
	model.search.cursor = 2
	model.nav.diffCursor = 3

	// press N to go to prev match
	result, _ = model.Update(tea.KeyPressMsg{Code: 'N', Text: string('N')})
	model = result.(Model)
	assert.Equal(t, 1, model.search.cursor, "search cursor should go back to 1")
	assert.Equal(t, 2, model.nav.diffCursor, "diff cursor should move to second match")
}

func TestModel_PrevSearchMatchWrapsAround(t *testing.T) {
	lines := []diff.DiffLine{
		{NewNum: 1, Content: "match one", ChangeType: diff.ChangeContext},
		{NewNum: 2, Content: "no match", ChangeType: diff.ChangeAdd},
		{NewNum: 3, Content: "match two", ChangeType: diff.ChangeContext},
	}
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := result.(Model)
	result, _ = model.Update(fileLoadedMsg{file: "a.go", lines: lines})
	model = result.(Model)
	model.layout.focus = paneDiff
	model.search.matches = []int{0, 2}
	model.search.cursor = 0 // on first match
	model.nav.diffCursor = 0

	// press N should wrap to last match
	result, _ = model.Update(tea.KeyPressMsg{Code: 'N', Text: string('N')})
	model = result.(Model)
	assert.Equal(t, 1, model.search.cursor, "search cursor should wrap to last")
	assert.Equal(t, 2, model.nav.diffCursor, "diff cursor should wrap to last match")
}

func TestModel_SearchNavigationSkipsCollapsedHiddenLines(t *testing.T) {
	// in collapsed mode, removed lines are hidden. search navigation must skip them.
	lines := []diff.DiffLine{
		{NewNum: 1, Content: "match ctx", ChangeType: diff.ChangeContext},
		{OldNum: 2, Content: "match removed", ChangeType: diff.ChangeRemove},
		{NewNum: 2, Content: "match added", ChangeType: diff.ChangeAdd},
		{NewNum: 3, Content: "match end", ChangeType: diff.ChangeContext},
	}
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := result.(Model)
	result, _ = model.Update(fileLoadedMsg{file: "a.go", lines: lines})
	model = result.(Model)
	model.layout.focus = paneDiff
	model.modes.collapsed.enabled = true
	model.modes.collapsed.expandedHunks = make(map[int]bool)
	// matches on indices 0 (ctx), 1 (hidden remove), 2 (add), 3 (ctx)
	model.search.matches = []int{0, 1, 2, 3}
	model.search.cursor = 0
	model.nav.diffCursor = 0

	t.Run("nextSearchMatch skips hidden removed line", func(t *testing.T) {
		m := model
		m.nextSearchMatch()
		assert.Equal(t, 2, m.search.cursor, "should skip hidden index 1, land on index 2")
		assert.Equal(t, 2, m.nav.diffCursor, "cursor should be on visible add line")
	})

	t.Run("prevSearchMatch skips hidden removed line", func(t *testing.T) {
		m := model
		m.search.cursor = 2 // on index 2 (add line)
		m.nav.diffCursor = 2
		m.prevSearchMatch()
		assert.Equal(t, 0, m.search.cursor, "should skip hidden index 1, land on index 0")
		assert.Equal(t, 0, m.nav.diffCursor, "cursor should be on visible context line")
	})

	t.Run("submitSearch skips hidden match for initial jump", func(t *testing.T) {
		m := model
		m.nav.diffCursor = 1 // cursor on hidden line
		m.search.term = ""
		m.search.matches = nil
		m.search.input = textinput.New()
		m.search.input.SetValue("match")
		m.submitSearch()
		// should jump to index 2 (visible add) not index 1 (hidden remove)
		assert.Equal(t, 2, m.nav.diffCursor, "should skip hidden remove and land on visible add")
	})
}

func TestModel_NKeyFallsThroughToNextFileWhenNoSearch(t *testing.T) {
	lines := []diff.DiffLine{{NewNum: 1, Content: "line1", ChangeType: diff.ChangeContext}}
	m := testModel([]string{"a.go", "b.go"}, map[string][]diff.DiffLine{
		"a.go": lines, "b.go": lines,
	})
	m.tree = testNewFileTree([]string{"a.go", "b.go"})
	m.file.name = "a.go"
	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := result.(Model)

	// no search active, n should advance to next file
	assert.Empty(t, model.search.matches)
	result, _ = model.Update(tea.KeyPressMsg{Code: 'n', Text: string('n')})
	model = result.(Model)
	assert.Equal(t, "b.go", model.tree.SelectedFile(), "n should go to next file when no search active")
}

func TestModel_ShiftNDoesPrevMatchWhenSearchActive(t *testing.T) {
	lines := []diff.DiffLine{
		{NewNum: 1, Content: "match one", ChangeType: diff.ChangeContext},
		{NewNum: 2, Content: "match two", ChangeType: diff.ChangeAdd},
	}
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := result.(Model)
	result, _ = model.Update(fileLoadedMsg{file: "a.go", lines: lines})
	model = result.(Model)
	model.layout.focus = paneDiff
	model.search.matches = []int{0, 1}
	model.search.cursor = 1
	model.nav.diffCursor = 1

	// press N (shift-n)
	result, _ = model.Update(tea.KeyPressMsg{Code: 'N', Text: string('N')})
	model = result.(Model)
	assert.Equal(t, 0, model.search.cursor, "N should go to prev match")
	assert.Equal(t, 0, model.nav.diffCursor, "cursor should be on first match")
}

func TestModel_ShiftNNavigatesPrevFileWithoutSearch(t *testing.T) {
	lines := []diff.DiffLine{{NewNum: 1, Content: "line1", ChangeType: diff.ChangeContext}}
	m := testModel([]string{"a.go", "b.go"}, map[string][]diff.DiffLine{
		"a.go": lines, "b.go": lines,
	})
	m.tree = testNewFileTree([]string{"a.go", "b.go"})
	m.tree.SelectByPath("b.go") // start at second file
	m.file.name = "b.go"

	// no search active, N (prev_item) should navigate to previous file
	assert.Empty(t, m.search.matches)
	result, _ := m.Update(tea.KeyPressMsg{Code: 'N', Text: string('N')})
	model := result.(Model)
	assert.Equal(t, "a.go", model.tree.SelectedFile(), "N should navigate to previous file")
}

func TestModel_SearchHighlightInRenderDiff(t *testing.T) {
	lines := []diff.DiffLine{
		{NewNum: 1, Content: "package main", ChangeType: diff.ChangeContext},
		{NewNum: 2, Content: "func hello() {}", ChangeType: diff.ChangeAdd},
		{NewNum: 3, Content: "func world() {}", ChangeType: diff.ChangeAdd},
		{OldNum: 4, Content: "old line", ChangeType: diff.ChangeRemove},
	}
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	m.tree = testNewFileTree([]string{"a.go"})
	m.file.name = "a.go"
	m.file.lines = lines
	m.file.highlighted = noopHighlighter().HighlightLines("a.go", lines)
	m.layout.focus = paneDiff
	m.nav.diffCursor = 0
	pRes := style.PlainResolver()
	m.resolver = pRes
	m.renderer = style.NewRenderer(pRes)

	t.Run("no search, renderDiff succeeds with all lines", func(t *testing.T) {
		m.search.matches = nil
		m.search.matchSet = nil
		rendered := m.renderDiff()
		assert.Contains(t, rendered, "package main")
		assert.Contains(t, ansi.Strip(rendered), "func hello")
		assert.Contains(t, rendered, "func world")
		assert.Contains(t, rendered, "old line")
	})

	t.Run("search active, renderDiff includes matched content", func(t *testing.T) {
		m.search.term = "hello"
		m.search.matches = []int{1}
		m.search.cursor = 0
		rendered := m.renderDiff()
		// matched and non-matched lines should both be rendered
		assert.Contains(t, ansi.Strip(rendered), "func hello")
		assert.Contains(t, rendered, "func world")
		assert.Contains(t, rendered, "old line")
	})

	t.Run("search vs no search both render content correctly", func(t *testing.T) {
		m.search.term = "hello"
		m.search.matches = []int{1}
		m.search.cursor = 0
		renderedWithSearch := m.renderDiff()

		m.search.matches = nil
		renderedWithout := m.renderDiff()

		// both should contain the same text content
		assert.Contains(t, ansi.Strip(renderedWithSearch), "func hello")
		assert.Contains(t, ansi.Strip(renderedWithout), "func hello")
		assert.Contains(t, renderedWithSearch, "func world")
		assert.Contains(t, renderedWithout, "func world")
	})

	t.Run("cursor coexists with search highlight", func(t *testing.T) {
		m.search.term = "hello"
		m.search.matches = []int{1}
		m.search.cursor = 0
		m.nav.diffCursor = 1
		rendered := m.renderDiff()

		outputLines := strings.Split(rendered, "\n")
		var matchLine string
		for _, l := range outputLines {
			if strings.Contains(l, "hello") {
				matchLine = l
			}
		}
		require.NotEmpty(t, matchLine)
		assert.Contains(t, matchLine, "▶", "cursor should be present on matched line")
		assert.Contains(t, ansi.Strip(matchLine), "func hello", "content should be preserved with cursor on match")
	})
}

func TestModel_SearchHighlightWithWrap(t *testing.T) {
	longContent := "this is a very long line that contains the search term hello somewhere in the middle and should wrap"
	lines := []diff.DiffLine{
		{NewNum: 1, Content: longContent, ChangeType: diff.ChangeAdd},
		{NewNum: 2, Content: "short line", ChangeType: diff.ChangeAdd},
	}
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	m.tree = testNewFileTree([]string{"a.go"})
	m.file.name = "a.go"
	m.file.lines = lines
	m.file.highlighted = noopHighlighter().HighlightLines("a.go", lines)
	m.layout.focus = paneDiff
	m.nav.diffCursor = 0
	m.modes.wrap = true
	m.layout.width = 60
	m.layout.treeWidth = 12
	pRes := style.PlainResolver()
	m.resolver = pRes
	m.renderer = style.NewRenderer(pRes)

	m.search.term = "hello"
	m.search.matches = []int{0}
	m.search.cursor = 0

	rendered := m.renderDiff()
	outputLines := strings.Split(strings.TrimSuffix(rendered, "\n"), "\n")

	// the long line should produce continuation rows with ↪
	var continuationCount int
	for _, l := range outputLines {
		if strings.Contains(l, "↪") {
			continuationCount++
		}
	}
	assert.Positive(t, continuationCount, "wrapped search match should have continuation lines")

	// verify content is present (text flows through the rendering path correctly)
	assert.Contains(t, rendered, "hello")
	assert.Contains(t, rendered, "short line")
}

func TestModel_SearchHighlightInCollapsedMode(t *testing.T) {
	lines := []diff.DiffLine{
		{NewNum: 1, Content: "context line", ChangeType: diff.ChangeContext},
		{OldNum: 2, Content: "removed line", ChangeType: diff.ChangeRemove},
		{NewNum: 2, Content: "added hello line", ChangeType: diff.ChangeAdd},
		{NewNum: 3, Content: "added other line", ChangeType: diff.ChangeAdd},
	}
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	m.tree = testNewFileTree([]string{"a.go"})
	m.file.name = "a.go"
	m.file.lines = lines
	m.file.highlighted = noopHighlighter().HighlightLines("a.go", lines)
	m.layout.focus = paneDiff
	m.nav.diffCursor = 0
	pRes := style.PlainResolver()
	m.resolver = pRes
	m.renderer = style.NewRenderer(pRes)
	m.modes.collapsed.enabled = true
	m.modes.collapsed.expandedHunks = make(map[int]bool)

	t.Run("collapsed renders with search matches", func(t *testing.T) {
		m.search.term = "hello"
		m.search.matches = []int{2}
		m.search.cursor = 0
		rendered := m.renderDiff()

		assert.Contains(t, ansi.Strip(rendered), "added hello line")
		assert.Contains(t, rendered, "added other line")
	})

	t.Run("collapsed without search has no match set", func(t *testing.T) {
		m.search.matches = nil
		m.search.matchSet = nil
		rendered := m.renderDiff()

		assert.Contains(t, rendered, "added hello line")
		assert.Nil(t, m.search.matchSet, "no search should produce nil match set")
	})
}

func TestModel_StyleDiffContentSearchMatch(t *testing.T) {
	m := testModel(nil, nil)
	pRes := style.PlainResolver()
	m.resolver = pRes
	m.renderer = style.NewRenderer(pRes)

	t.Run("search match returns same text content", func(t *testing.T) {
		resultMatch := m.styleDiffContent(diff.ChangeAdd, " + ", "content", false, true)
		resultNoMatch := m.styleDiffContent(diff.ChangeAdd, " + ", "content", false, false)
		assert.Contains(t, resultMatch, " + content")
		assert.Contains(t, resultNoMatch, " + content")
	})

	t.Run("search match with highlight preserves content", func(t *testing.T) {
		result := m.styleDiffContent(diff.ChangeAdd, " + ", "\033[32mgreen\033[0m", true, true)
		assert.Contains(t, result, " + ")
		assert.Contains(t, result, "\033[32m", "chroma foreground should be preserved")
	})

	t.Run("search match uses different style than normal add", func(t *testing.T) {
		// use resolver with distinct colors so rendering produces different output
		c := style.Colors{
			Accent: "#ffffff", Border: "#555555", Normal: "#cccccc", Muted: "#666666",
			SelectedFg: "#ffffff", SelectedBg: "#333333", Annotation: "#ff9900",
			AddFg: "#00ff00", AddBg: "#002200", RemoveFg: "#ff0000", RemoveBg: "#220000",
			ModifyFg: "#ffaa00", ModifyBg: "#221100",
			SearchFg: "#1a1a1a", SearchBg: "#d7d700",
		}
		m.resolver = style.NewResolver(c)
		resultMatch := m.styleDiffContent(diff.ChangeAdd, " + ", "content", false, true)
		resultNoMatch := m.styleDiffContent(diff.ChangeAdd, " + ", "content", false, false)
		// both have same text but may differ in ANSI sequences (depends on terminal detection)
		// the key test is that both contain the content and the code paths don't panic
		assert.Contains(t, resultMatch, "content")
		assert.Contains(t, resultNoMatch, "content")
	})
}

func TestModel_BuildSearchMatchSet(t *testing.T) {
	m := testModel(nil, nil)

	t.Run("empty matches produces nil set", func(t *testing.T) {
		m.search.matches = nil
		result := m.buildSearchMatchSet()
		assert.Nil(t, result)
	})

	t.Run("matches produce correct set", func(t *testing.T) {
		m.search.matches = []int{1, 5, 10}
		result := m.buildSearchMatchSet()
		assert.True(t, result[1])
		assert.True(t, result[5])
		assert.True(t, result[10])
		assert.False(t, result[0])
		assert.False(t, result[3])
	})
}

func TestModel_ClearSearchResetsMatchSet(t *testing.T) {
	m := testModel(nil, nil)
	m.search.term = "test"
	m.search.matches = []int{1, 2}
	m.search.cursor = 1
	m.search.matchSet = map[int]bool{1: true, 2: true}

	m.clearSearch()

	assert.Empty(t, m.search.term)
	assert.Nil(t, m.search.matches)
	assert.Equal(t, 0, m.search.cursor)
	assert.Nil(t, m.search.matchSet)
}

func TestModel_SearchUsesCommandPane(t *testing.T) {
	for _, noStatus := range []bool{false, true} {
		for _, fromCommand := range []bool{false, true} {
			m := testModel([]string{"a.go"}, nil)
			m.file.name, m.file.singleFile, m.layout.focus = "a.go", true, paneDiff
			m.file.lines = []diff.DiffLine{
				{NewNum: 1, Content: "first", ChangeType: diff.ChangeContext},
				{NewNum: 2, Content: "needle here", ChangeType: diff.ChangeContext},
				{NewNum: 3, Content: "gap", ChangeType: diff.ChangeContext},
				{NewNum: 4, Content: "another needle", ChangeType: diff.ChangeContext},
			}
			m.cfg.noStatusBar = noStatus
			m.review.cfg = &ReviewInfoConfig{VCS: "git", WorkDir: "/work/review"}
			m.live.sender = &feedbackStub{harness: "amp", display: "Review search T-review"}
			model, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
			m = model.(Model)
			originalHeight := m.layout.viewport.Height()
			if fromCommand {
				m.startCommand()
				m.command.input.SetValue("search")
				model, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			} else {
				model, _ = m.Update(tea.KeyPressMsg{Text: "/"})
			}
			m = model.(Model)
			require.True(t, m.search.active)
			require.False(t, m.command.active)
			require.Equal(t, originalHeight-4, m.layout.viewport.Height(), "one palette, not two stacked panes")
			m.search.history = []string{"first", "needle"}
			m.search.historyIdx = 2
			model, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
			m = model.(Model)
			require.Equal(t, "needle", m.search.input.Value())
			for _, width := range []int{30, 100} {
				model, _ = m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
				m = model.(Model)
				require.Equal(t, 6, m.search.input.Position())
				require.Equal(t, width-5, m.search.input.Width())
				require.Equal(t, 4, lipgloss.Height(m.commandPaneView()))
				for _, row := range strings.Split(m.commandPaneView(), "\n") {
					require.LessOrEqual(t, ansi.StringWidth(row), width)
				}
			}
			view := ansi.Strip(m.View().Content)
			require.Equal(t, 30, lipgloss.Height(view))
			require.Equal(t, 1, strings.Count(view, "/needle"))
			require.Contains(t, view, "Search · Enter find")
			require.NotContains(t, m.statusBarText(), "/needle")
			if !noStatus {
				require.Contains(t, view, "Harness (amp): Review search T-review")
				require.NotContains(t, view, "Repository:")
				require.Contains(t, m.statusBarText(), "a.go")
			}
			model, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = model.(Model)
			require.False(t, m.search.active)
			require.False(t, m.search.input.Focused())
			require.Equal(t, originalHeight, m.layout.viewport.Height())
			require.Equal(t, []int{1, 3}, m.search.matches)
			require.Equal(t, 1, m.nav.diffCursor)
			model, _ = m.Update(tea.KeyPressMsg{Text: "n"})
			m = model.(Model)
			require.Equal(t, 3, m.nav.diffCursor)
			for _, cancel := range []tea.KeyPressMsg{tea.KeyPressMsg{Code: tea.KeyEsc}, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}} {
				m.startSearch()
				model, _ = m.Update(tea.KeyPressMsg{Text: ":quit"})
				m = model.(Model)
				require.Equal(t, ":quit", m.search.input.Value(), "commands stay literal inside search")
				model, _ = m.Update(cancel)
				m = model.(Model)
				require.False(t, m.search.active)
				require.Equal(t, "needle", m.search.term, "cancel preserves the previous search")
				require.Equal(t, originalHeight, m.layout.viewport.Height())
			}
		}
	}
}

func TestModel_StatusBarSearchMatchPosition(t *testing.T) {
	tests := []struct {
		name         string
		matches      []int
		cursor       int
		wantContains string
		wantAbsent   string
	}{
		{name: "first of three", matches: []int{0, 2, 5}, cursor: 0, wantContains: "1/3"},
		{name: "second of three", matches: []int{0, 2, 5}, cursor: 1, wantContains: "2/3"},
		{name: "third of three", matches: []int{0, 2, 5}, cursor: 2, wantContains: "3/3"},
		{name: "single match", matches: []int{1}, cursor: 0, wantContains: "1/1"},
		{name: "no matches", matches: nil, cursor: 0, wantAbsent: "["},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := testModel(nil, nil)
			m.file.name = "a.go"
			m.file.lines = []diff.DiffLine{
				{NewNum: 1, Content: "ctx", ChangeType: diff.ChangeContext},
				{NewNum: 2, Content: "add", ChangeType: diff.ChangeAdd},
				{NewNum: 3, Content: "ctx2", ChangeType: diff.ChangeContext},
			}
			m.layout.focus = paneDiff
			m.layout.width = 200
			m.search.matches = tt.matches
			m.search.cursor = tt.cursor

			status := m.statusBarText()
			if tt.wantContains != "" {
				assert.Contains(t, status, tt.wantContains)
			}
			if tt.wantAbsent != "" {
				assert.NotContains(t, status, tt.wantAbsent)
			}
		})
	}
}

func TestModel_SearchSegment(t *testing.T) {
	m := testModel(nil, nil)

	// no matches
	assert.Empty(t, m.searchSegment())

	// with matches
	m.search.matches = []int{0, 3, 7}
	m.search.cursor = 1
	assert.Equal(t, "2/3", m.searchSegment())

	// all matches on hidden removed lines in collapsed mode shows [0/N]
	lines := []diff.DiffLine{
		{NewNum: 1, Content: "ctx", ChangeType: diff.ChangeContext},
		{OldNum: 2, Content: "removed match", ChangeType: diff.ChangeRemove},
		{NewNum: 2, Content: "added", ChangeType: diff.ChangeAdd},
		{NewNum: 3, Content: "ctx end", ChangeType: diff.ChangeContext},
	}
	m2 := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	m2.file.lines = lines
	m2.file.name = "a.go"
	m2.modes.collapsed.enabled = true
	m2.modes.collapsed.expandedHunks = make(map[int]bool)
	m2.search.matches = []int{1} // only on hidden removed line
	m2.search.cursor = 0
	assert.Equal(t, "0/1", m2.searchSegment(), "should show [0/N] when all matches are hidden")
}

func TestModel_StatusBarSearchPositionBetweenHunkAndIcons(t *testing.T) {
	lines := []diff.DiffLine{
		{NewNum: 1, Content: "ctx", ChangeType: diff.ChangeContext},
		{NewNum: 2, Content: "add", ChangeType: diff.ChangeAdd},
	}
	m := testModel(nil, nil)
	m.file.name = "a.go"
	m.file.lines = lines
	m.nav.diffCursor = 1
	m.file.adds = 1
	m.layout.focus = paneDiff
	m.layout.width = 200
	m.search.matches = []int{1}
	m.search.cursor = 0
	m.modes.collapsed.enabled = true
	m.modes.collapsed.expandedHunks = make(map[int]bool)

	status := m.statusBarText()
	// all three should be present
	assert.Contains(t, status, "hunk 1/1")
	assert.Contains(t, status, "1/1")
	assert.Contains(t, status, "▼")

	// [1/1] should appear after hunk and before ▼
	hunkIdx := strings.Index(status, "hunk 1/1")
	searchIdx := strings.Index(status, "1/1")
	iconIdx := strings.Index(status, "▼")
	assert.Greater(t, searchIdx, hunkIdx, "search position should appear after hunk")
	assert.Less(t, searchIdx, iconIdx, "search position should appear before mode icons")
}

func TestModel_RefreshSearchOnFileLoad(t *testing.T) {
	lines1 := []diff.DiffLine{
		{NewNum: 1, Content: "hello world", ChangeType: diff.ChangeContext},
		{NewNum: 2, Content: "hello again", ChangeType: diff.ChangeAdd},
	}
	lines2 := []diff.DiffLine{
		{NewNum: 1, Content: "other content", ChangeType: diff.ChangeContext},
		{NewNum: 2, Content: "HELLO elsewhere", ChangeType: diff.ChangeAdd},
	}
	m := testModel([]string{"a.go", "b.go"}, map[string][]diff.DiffLine{"a.go": lines1, "b.go": lines2})
	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := result.(Model)
	result, _ = model.Update(fileLoadedMsg{file: "a.go", seq: model.file.loadSeq, lines: lines1})
	model = result.(Model)
	model.layout.focus = paneDiff

	// set up search state as if user searched for "hello"
	model.search.term = "hello"
	model.search.matches = []int{0, 1}
	model.search.cursor = 1
	model.search.matchSet = map[int]bool{0: true, 1: true}

	// load a different file
	model.file.loadSeq++
	result, _ = model.Update(fileLoadedMsg{file: "b.go", seq: model.file.loadSeq, lines: lines2})
	model = result.(Model)

	assert.Equal(t, "hello", model.search.term, "query survives file navigation")
	assert.Equal(t, []int{1}, model.search.matches, "matches are recomputed for the loaded file")
	assert.Equal(t, 0, model.search.cursor, "search cursor should be reset on file load")
	assert.Nil(t, model.search.matchSet, "search match set should be cleared on file load")
}

func TestModel_StatusBarNarrowDropsSearchSegment(t *testing.T) {
	lines := []diff.DiffLine{
		{NewNum: 1, Content: "ctx", ChangeType: diff.ChangeContext},
		{NewNum: 2, Content: "add", ChangeType: diff.ChangeAdd},
	}
	m := testModel(nil, nil)
	m.file.name = "a.go"
	m.file.lines = lines
	m.nav.diffCursor = 1
	m.file.adds = 1
	m.layout.focus = paneDiff
	m.search.matches = []int{1}
	m.search.cursor = 0

	t.Run("wide terminal shows search segment", func(t *testing.T) {
		m.layout.width = 200
		status := m.statusBarText()
		assert.Contains(t, status, "1/1")
	})

	t.Run("very narrow terminal drops search with hunk", func(t *testing.T) {
		m.layout.width = 28
		status := m.statusBarText()
		assert.NotContains(t, status, "1/1", "search segment should be dropped on very narrow terminal")
		assert.Contains(t, status, "? help")
	})
}

func TestModel_RealignSearchCursorOnCollapsedToggle(t *testing.T) {
	// when toggling collapsed mode, searchCursor must realign to nearest visible match
	lines := []diff.DiffLine{
		{NewNum: 1, Content: "match ctx", ChangeType: diff.ChangeContext},
		{OldNum: 2, Content: "match removed", ChangeType: diff.ChangeRemove},
		{NewNum: 2, Content: "match added", ChangeType: diff.ChangeAdd},
		{NewNum: 3, Content: "match end", ChangeType: diff.ChangeContext},
	}
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := result.(Model)
	result, _ = model.Update(fileLoadedMsg{file: "a.go", lines: lines})
	model = result.(Model)
	model.layout.focus = paneDiff

	// set up search with cursor on the removed line (index 1)
	model.search.matches = []int{0, 1, 2, 3}
	model.search.cursor = 1
	model.nav.diffCursor = 1

	// toggle collapsed mode, which hides removed lines
	model.toggleCollapsedMode()

	assert.True(t, model.modes.collapsed.enabled)
	assert.NotEqual(t, 1, model.nav.diffCursor, "cursor should have moved off hidden removed line")
	assert.NotEqual(t, 1, model.search.cursor, "searchCursor should realign away from hidden match")
	// searchCursor should point to a visible match
	if model.search.cursor < len(model.search.matches) {
		matchIdx := model.search.matches[model.search.cursor]
		hunks := model.findHunks()
		assert.False(t, model.isCollapsedHidden(matchIdx, hunks), "realigned searchCursor should point to a visible match")
	}
}

func TestModel_RealignSearchCursorOnHunkCollapse(t *testing.T) {
	// when collapsing a hunk, searchCursor must realign if current match becomes hidden
	lines := []diff.DiffLine{
		{NewNum: 1, Content: "match ctx", ChangeType: diff.ChangeContext},
		{OldNum: 2, Content: "match removed", ChangeType: diff.ChangeRemove},
		{NewNum: 2, Content: "match added", ChangeType: diff.ChangeAdd},
		{NewNum: 3, Content: "ctx end", ChangeType: diff.ChangeContext},
	}
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := result.(Model)
	result, _ = model.Update(fileLoadedMsg{file: "a.go", lines: lines})
	model = result.(Model)
	model.layout.focus = paneDiff

	// start in collapsed mode with hunk expanded (hunk starts at index 1, first change line)
	model.modes.collapsed.enabled = true
	model.modes.collapsed.expandedHunks = map[int]bool{1: true}
	model.search.matches = []int{0, 1, 2, 3}
	model.search.cursor = 1 // on removed line (visible because hunk is expanded)
	model.nav.diffCursor = 1

	// collapse the hunk — removed line becomes hidden
	model.toggleHunkExpansion()

	assert.NotContains(t, model.modes.collapsed.expandedHunks, 1, "hunk should be collapsed")
	// searchCursor should have realigned to a visible match
	if len(model.search.matches) > 0 && model.search.cursor < len(model.search.matches) {
		matchIdx := model.search.matches[model.search.cursor]
		hunks := model.findHunks()
		assert.False(t, model.isCollapsedHidden(matchIdx, hunks), "searchCursor should point to visible match after hunk collapse")
	}
}

func TestModel_RealignSearchCursorNoopWithoutSearch(t *testing.T) {
	// realignSearchCursor should be a no-op when no search is active
	lines := []diff.DiffLine{
		{NewNum: 1, Content: "context", ChangeType: diff.ChangeContext},
		{OldNum: 2, Content: "removed", ChangeType: diff.ChangeRemove},
	}
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := result.(Model)
	result, _ = model.Update(fileLoadedMsg{file: "a.go", lines: lines})
	model = result.(Model)
	model.layout.focus = paneDiff
	model.search.matches = nil
	model.search.cursor = 0

	// should not panic or change anything
	model.realignSearchCursor()
	assert.Equal(t, 0, model.search.cursor)
}

func TestModel_SubmitSearchPreservesLeadingWhitespace(t *testing.T) {
	// search query with leading/trailing whitespace should be preserved in the search term
	lines := []diff.DiffLine{
		{NewNum: 1, Content: "  indented line", ChangeType: diff.ChangeContext},
		{NewNum: 2, Content: "normal line", ChangeType: diff.ChangeContext},
	}
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := result.(Model)
	result, _ = model.Update(fileLoadedMsg{file: "a.go", lines: lines})
	model = result.(Model)
	model.layout.focus = paneDiff

	model.search.input = textinput.New()
	model.search.input.SetValue("  indented")
	model.submitSearch()

	assert.Equal(t, "  indented", model.search.term, "leading whitespace should be preserved in search term")
	assert.Equal(t, []int{0}, model.search.matches, "should match the indented line")
}

func TestModel_SubmitSearchWhitespaceOnlyClearsSearch(t *testing.T) {
	// pure whitespace query should clear search (same as empty)
	lines := []diff.DiffLine{
		{NewNum: 1, Content: "line", ChangeType: diff.ChangeContext},
	}
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := result.(Model)
	result, _ = model.Update(fileLoadedMsg{file: "a.go", lines: lines})
	model = result.(Model)
	model.layout.focus = paneDiff

	// pre-populate search state
	model.search.term = "old"
	model.search.matches = []int{0}
	model.search.cursor = 0

	model.search.input = textinput.New()
	model.search.input.SetValue("   ")
	model.submitSearch()

	assert.Empty(t, model.search.term, "whitespace-only query should clear search")
	assert.Nil(t, model.search.matches)
}

func TestModel_DeletePlaceholderSearchHighlight(t *testing.T) {
	// delete-only placeholder should render correctly with and without search match.
	// verifies the code path doesn't panic and produces correct text content.
	// (actual ANSI styling differences depend on terminal detection)
	lines := []diff.DiffLine{
		{NewNum: 1, Content: "context", ChangeType: diff.ChangeContext},
		{OldNum: 2, Content: "deleted match", ChangeType: diff.ChangeRemove},
		{OldNum: 3, Content: "deleted other", ChangeType: diff.ChangeRemove},
		{NewNum: 2, Content: "context end", ChangeType: diff.ChangeContext},
	}
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := result.(Model)
	result, _ = model.Update(fileLoadedMsg{file: "a.go", lines: lines})
	model = result.(Model)
	model.layout.focus = paneDiff
	plainRes := style.PlainResolver()
	model.resolver = plainRes
	model.renderer = style.NewRenderer(plainRes)
	model.modes.collapsed.enabled = true
	model.modes.collapsed.expandedHunks = make(map[int]bool)
	model.nav.diffCursor = 1

	t.Run("with search match", func(t *testing.T) {
		model.search.matchSet = map[int]bool{1: true}
		var b strings.Builder
		model.renderDeletePlaceholder(&b, 1, 1)
		rendered := b.String()
		assert.Contains(t, rendered, "2 lines deleted")
		assert.Contains(t, rendered, "▶", "cursor indicator should be present")
	})

	t.Run("without search match", func(t *testing.T) {
		model.search.matchSet = nil
		var b strings.Builder
		model.renderDeletePlaceholder(&b, 1, 1)
		rendered := b.String()
		assert.Contains(t, rendered, "2 lines deleted")
		assert.Contains(t, rendered, "▶")
	})

	t.Run("with wrap mode and search match", func(t *testing.T) {
		model.search.matchSet = map[int]bool{1: true}
		model.modes.wrap = true
		model.layout.width = 120
		model.layout.treeWidth = 30
		var b strings.Builder
		model.renderDeletePlaceholder(&b, 1, 1)
		rendered := b.String()
		assert.Contains(t, rendered, "2 lines deleted")
		model.modes.wrap = false
	})
}

func TestModel_SearchWithTOCActive(t *testing.T) {
	mdLines := []diff.DiffLine{
		{NewNum: 1, Content: "# Title", ChangeType: diff.ChangeContext},
		{NewNum: 2, Content: "some text", ChangeType: diff.ChangeContext},
		{NewNum: 3, Content: "## Section", ChangeType: diff.ChangeContext},
		{NewNum: 4, Content: "more text with title", ChangeType: diff.ChangeContext},
	}

	t.Run("start search from diff pane with TOC", func(t *testing.T) {
		m := testModel([]string{"README.md"}, map[string][]diff.DiffLine{"README.md": mdLines})
		m.file.singleFile = true
		m.layout.treeWidth = 0

		result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		model := result.(Model)
		result, _ = model.Update(fileLoadedMsg{file: "README.md", lines: mdLines})
		model = result.(Model)
		require.NotNil(t, model.file.mdTOC)

		model.layout.focus = paneDiff

		// press '/' to start search
		result, _ = model.Update(tea.KeyPressMsg{Code: '/', Text: string('/')})
		model = result.(Model)
		assert.True(t, model.search.active, "should enter search mode in diff pane with TOC")
	})

	t.Run("search not started from TOC pane", func(t *testing.T) {
		m := testModel([]string{"README.md"}, map[string][]diff.DiffLine{"README.md": mdLines})
		m.file.singleFile = true
		m.layout.treeWidth = 0

		result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		model := result.(Model)
		result, _ = model.Update(fileLoadedMsg{file: "README.md", lines: mdLines})
		model = result.(Model)
		require.NotNil(t, model.file.mdTOC)

		model.layout.focus = paneTree // TOC pane

		// press '/' in TOC pane - should not start search
		result, _ = model.Update(tea.KeyPressMsg{Code: '/', Text: string('/')})
		model = result.(Model)
		assert.False(t, model.search.active, "search should not start from TOC pane")
	})

	t.Run("TOC active section updates after search navigation", func(t *testing.T) {
		m := testModel([]string{"README.md"}, map[string][]diff.DiffLine{"README.md": mdLines})
		m.file.singleFile = true
		m.layout.treeWidth = 0

		result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		model := result.(Model)
		result, _ = model.Update(fileLoadedMsg{file: "README.md", lines: mdLines})
		model = result.(Model)
		require.NotNil(t, model.file.mdTOC)

		model.layout.focus = paneDiff
		model.search.matches = []int{3} // match on line 3
		model.search.cursor = 0

		// navigate to search match via 'n' key
		result, _ = model.Update(tea.KeyPressMsg{Code: 'n', Text: string('n')})
		model = result.(Model)
		// active section should reflect the cursor position after search nav (Section entry at lineIdx=2)
		assert.Equal(t, 2, tocActiveLineIdx(t, model.file.mdTOC), "TOC should track active section after search jump (lineIdx=2)")
	})
}

// helper: build a model with the given diff lines and return it ready to search.
func newSearchHistoryModel(t *testing.T) Model {
	t.Helper()
	lines := []diff.DiffLine{
		{NewNum: 1, Content: "alpha line", ChangeType: diff.ChangeContext},
		{NewNum: 2, Content: "beta added", ChangeType: diff.ChangeAdd},
		{NewNum: 3, Content: "gamma context", ChangeType: diff.ChangeContext},
	}
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := result.(Model)
	result, _ = model.Update(fileLoadedMsg{file: "a.go", lines: lines})
	model = result.(Model)
	model.layout.focus = paneDiff
	return model
}

// helper: submit a query through submitSearch and return the resulting model.
func submitQueryThroughInput(model Model, query string) Model {
	model.search.active = true
	model.search.input = textinput.New()
	model.search.input.SetValue(query)
	model.submitSearch()
	return model
}

func TestModel_SearchHistory_AppendsOnSubmit(t *testing.T) {
	model := newSearchHistoryModel(t)

	model = submitQueryThroughInput(model, "alpha")
	model = submitQueryThroughInput(model, "beta")
	model = submitQueryThroughInput(model, "gamma")

	assert.Equal(t, []string{"alpha", "beta", "gamma"}, model.search.history)
	assert.Equal(t, len(model.search.history), model.search.historyIdx, "historyIdx should reset to draft slot after submit")
}

func TestModel_SearchHistory_ZeroMatchQueryStillAppended(t *testing.T) {
	// queries that match nothing must still be recallable so the user can edit and retry.
	// this verifies the append placement is BEFORE the match scan.
	model := newSearchHistoryModel(t)

	model = submitQueryThroughInput(model, "no-such-text")

	assert.Empty(t, model.search.matches, "query should produce zero matches")
	assert.Equal(t, []string{"no-such-text"}, model.search.history, "zero-match query must still appear in history")
}

func TestModel_SearchHistory_ConsecutiveDuplicatesNotAppended(t *testing.T) {
	model := newSearchHistoryModel(t)

	model = submitQueryThroughInput(model, "alpha")
	model = submitQueryThroughInput(model, "alpha")
	model = submitQueryThroughInput(model, "alpha")

	assert.Equal(t, []string{"alpha"}, model.search.history, "consecutive duplicates should be deduped")
}

func TestModel_SearchHistory_NonConsecutiveDuplicateAppends(t *testing.T) {
	// resubmitting an older entry moves it to "most recent".
	model := newSearchHistoryModel(t)

	model = submitQueryThroughInput(model, "alpha")
	model = submitQueryThroughInput(model, "beta")
	model = submitQueryThroughInput(model, "alpha")

	assert.Equal(t, []string{"alpha", "beta", "alpha"}, model.search.history)
}

func TestModel_SearchHistory_CapEnforced(t *testing.T) {
	model := newSearchHistoryModel(t)

	// push searchHistoryMax+1 unique entries; oldest must be dropped.
	for i := range searchHistoryMax + 1 {
		q := fmt.Sprintf("q%d", i)
		model = submitQueryThroughInput(model, q)
	}

	require.Len(t, model.search.history, searchHistoryMax)
	assert.Equal(t, "q1", model.search.history[0], "oldest entry q0 should have been dropped")
	newest := fmt.Sprintf("q%d", searchHistoryMax)
	assert.Equal(t, newest, model.search.history[searchHistoryMax-1], "newest entry should be at the tail")
}

func TestModel_SearchHistory_WhitespaceOnlyNotAppended(t *testing.T) {
	model := newSearchHistoryModel(t)

	model.search.active = true
	model.search.input = textinput.New()
	model.search.input.SetValue("   ")
	model.submitSearch()

	assert.Empty(t, model.search.history, "whitespace-only query should not be added to history")
}

func TestModel_SearchHistory_ClearSearchPreservesHistory(t *testing.T) {
	model := newSearchHistoryModel(t)

	model = submitQueryThroughInput(model, "alpha")
	model = submitQueryThroughInput(model, "beta")
	require.Len(t, model.search.history, 2)
	idxBefore := model.search.historyIdx

	model.clearSearch()

	assert.Equal(t, []string{"alpha", "beta"}, model.search.history, "history must survive clearSearch")
	assert.Equal(t, idxBefore, model.search.historyIdx, "historyIdx must survive clearSearch")
}

func TestModel_SearchHistory_StartSearchResetsIdxToDraft(t *testing.T) {
	model := newSearchHistoryModel(t)

	model = submitQueryThroughInput(model, "alpha")
	model = submitQueryThroughInput(model, "beta")
	// simulate partial recall having moved idx away from draft.
	model.search.historyIdx = 0

	model.startSearch()

	assert.Equal(t, len(model.search.history), model.search.historyIdx, "startSearch must reset idx to draft slot")
}

func TestModel_SearchHistory_RecallEmptyHistoryNoop(t *testing.T) {
	model := newSearchHistoryModel(t)
	model.search.input = textinput.New()
	// pre-set a non-zero sentinel so the no-op contract is verifiable
	// (otherwise an unconditional reset to 0 would also pass the assertion).
	const sentinel = 7
	model.search.historyIdx = sentinel

	// no submitted queries yet; recall in either direction must be a no-op.
	model.recallHistory(-1)
	assert.Empty(t, model.search.input.Value(), "input should remain empty")
	assert.Equal(t, sentinel, model.search.historyIdx, "idx should not change with empty history")

	model.recallHistory(+1)
	assert.Empty(t, model.search.input.Value())
	assert.Equal(t, sentinel, model.search.historyIdx)
}

func TestModel_SearchHistory_UpRecallsThroughOlderEntries(t *testing.T) {
	model := newSearchHistoryModel(t)

	model = submitQueryThroughInput(model, "alpha")
	model = submitQueryThroughInput(model, "beta")
	model = submitQueryThroughInput(model, "gamma")
	// simulate user re-entering search prompt.
	model.startSearch()

	// first Up: most recent = "gamma".
	model.recallHistory(-1)
	assert.Equal(t, "gamma", model.search.input.Value())

	// second Up: "beta".
	model.recallHistory(-1)
	assert.Equal(t, "beta", model.search.input.Value())

	// third Up: "alpha".
	model.recallHistory(-1)
	assert.Equal(t, "alpha", model.search.input.Value())
}

func TestModel_SearchHistory_UpAtOldestStaysSticky(t *testing.T) {
	model := newSearchHistoryModel(t)

	model = submitQueryThroughInput(model, "alpha")
	model = submitQueryThroughInput(model, "beta")
	model.startSearch()

	model.recallHistory(-1) // beta
	model.recallHistory(-1) // alpha (oldest)
	require.Equal(t, "alpha", model.search.input.Value())
	require.Equal(t, 0, model.search.historyIdx)

	// further Up at oldest is a sticky no-op.
	model.recallHistory(-1)
	assert.Equal(t, 0, model.search.historyIdx, "idx should stay at 0")
	assert.Equal(t, "alpha", model.search.input.Value(), "input should still hold oldest entry")
}

func TestModel_SearchHistory_DownPastNewestClearsInput(t *testing.T) {
	model := newSearchHistoryModel(t)

	model = submitQueryThroughInput(model, "alpha")
	model = submitQueryThroughInput(model, "beta")
	model.startSearch()

	model.recallHistory(-1) // beta
	require.Equal(t, "beta", model.search.input.Value())

	// Down once: past newest → draft slot, input cleared.
	model.recallHistory(+1)
	assert.Empty(t, model.search.input.Value(), "down past newest should clear input")
	assert.Equal(t, len(model.search.history), model.search.historyIdx, "idx should be at draft slot")

	// further Down stays at draft slot.
	model.recallHistory(+1)
	assert.Empty(t, model.search.input.Value())
	assert.Equal(t, len(model.search.history), model.search.historyIdx)
}

func TestModel_SearchHistory_UpDownInteractive(t *testing.T) {
	// exercise the same path through bubbletea Update so KeyUp/KeyDown wiring is covered.
	model := newSearchHistoryModel(t)

	model = submitQueryThroughInput(model, "alpha")
	model = submitQueryThroughInput(model, "beta")

	// re-enter search prompt via '/'.
	result, _ := model.Update(tea.KeyPressMsg{Code: '/', Text: string('/')})
	model = result.(Model)
	require.True(t, model.search.active)

	// Up: most recent = "beta".
	result, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	model = result.(Model)
	assert.Equal(t, "beta", model.search.input.Value(), "KeyUp should recall most recent")

	// Up: "alpha".
	result, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	model = result.(Model)
	assert.Equal(t, "alpha", model.search.input.Value())

	// Down: back to "beta".
	result, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = result.(Model)
	assert.Equal(t, "beta", model.search.input.Value())

	// Down: draft slot, input cleared.
	result, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = result.(Model)
	assert.Empty(t, model.search.input.Value(), "second KeyDown should clear input at draft slot")
}

func TestModel_SearchHistory_CtrlPCtrlNParity(t *testing.T) {
	// Ctrl+P / Ctrl+N must behave identically to Up / Down.
	model := newSearchHistoryModel(t)

	model = submitQueryThroughInput(model, "alpha")
	model = submitQueryThroughInput(model, "beta")

	result, _ := model.Update(tea.KeyPressMsg{Code: '/', Text: string('/')})
	model = result.(Model)

	// Ctrl+P = Up.
	result, _ = model.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	model = result.(Model)
	assert.Equal(t, "beta", model.search.input.Value())
	assert.True(t, model.search.active)
	assert.False(t, model.overlay.Active(), "Ctrl+P in search must recall history, not open the file picker")

	result, _ = model.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	model = result.(Model)
	assert.Equal(t, "alpha", model.search.input.Value())
	assert.True(t, model.search.active)
	assert.False(t, model.overlay.Active())

	// Ctrl+N = Down.
	result, _ = model.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	model = result.(Model)
	assert.Equal(t, "beta", model.search.input.Value())

	result, _ = model.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	model = result.(Model)
	assert.Empty(t, model.search.input.Value(), "Ctrl+N past newest should clear input")
}

// P is the default jump_file binding, so an active search prompt must swallow
// it as a literal rune instead of opening the file picker underneath.
func TestModel_SearchPrompt_SwallowsJumpFileKey(t *testing.T) {
	model := newSearchHistoryModel(t)

	result, _ := model.Update(tea.KeyPressMsg{Code: '/', Text: string('/')})
	model = result.(Model)

	result, _ = model.Update(tea.KeyPressMsg{Code: 'P', Text: string('P')})
	model = result.(Model)

	assert.Equal(t, "P", model.search.input.Value(), "P must type into the search prompt")
	assert.True(t, model.search.active)
	assert.False(t, model.overlay.Active(), "P in search must not open the file picker")
}

func TestModel_SearchHistory_RecallThenEscThenStartFresh(t *testing.T) {
	// recall → Esc → '/' again. the input must be empty (draft slot), not the recalled value.
	model := newSearchHistoryModel(t)

	model = submitQueryThroughInput(model, "alpha")
	model = submitQueryThroughInput(model, "beta")

	// '/' to enter search.
	result, _ := model.Update(tea.KeyPressMsg{Code: '/', Text: string('/')})
	model = result.(Model)

	// Up: recalled value appears.
	result, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	model = result.(Model)
	require.Equal(t, "beta", model.search.input.Value())

	// Esc: cancel without submitting.
	result, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model = result.(Model)
	require.False(t, model.search.active)

	// '/' again: input must be empty at draft slot, history unchanged.
	result, _ = model.Update(tea.KeyPressMsg{Code: '/', Text: string('/')})
	model = result.(Model)
	assert.True(t, model.search.active)
	assert.Empty(t, model.search.input.Value(), "after recall+Esc+/, input must be empty (draft slot)")
	assert.Equal(t, len(model.search.history), model.search.historyIdx, "idx should be at draft slot after fresh /")
	assert.Equal(t, []string{"alpha", "beta"}, model.search.history, "history should be unchanged by recall+Esc")
}

func TestModel_SearchHistory_RecalledThenSubmittedAppendsAgain(t *testing.T) {
	// recalling an older entry and pressing Enter appends it again (moves to most recent),
	// unless it is already the most recent (dedup).
	model := newSearchHistoryModel(t)

	model = submitQueryThroughInput(model, "alpha")
	model = submitQueryThroughInput(model, "beta")
	require.Equal(t, []string{"alpha", "beta"}, model.search.history)

	// re-enter search, recall "alpha" (older), submit.
	result, _ := model.Update(tea.KeyPressMsg{Code: '/', Text: string('/')})
	model = result.(Model)
	result, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyUp}) // beta
	model = result.(Model)
	result, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyUp}) // alpha
	model = result.(Model)
	require.Equal(t, "alpha", model.search.input.Value())
	result, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = result.(Model)

	assert.Equal(t, []string{"alpha", "beta", "alpha"}, model.search.history, "resubmitted older entry moves to most recent")
}

func treeSearchModel(t *testing.T) Model {
	t.Helper()
	files := map[string][]diff.DiffLine{
		"a.go": {
			{NewNum: 1, Content: "needle first", ChangeType: diff.ChangeContext},
			{NewNum: 2, Content: "gap", ChangeType: diff.ChangeContext},
			{NewNum: 3, Content: "NEEDLE second", ChangeType: diff.ChangeAdd},
		},
		"b.go": {{NewNum: 1, Content: "no match", ChangeType: diff.ChangeContext}},
		"c.go": {
			{NewNum: 1, Content: "different gap", ChangeType: diff.ChangeContext},
			{NewNum: 2, Content: "needle third", ChangeType: diff.ChangeAdd},
		},
	}
	m := testModel([]string{"a.go", "b.go", "c.go"}, files)
	m.tree.Rebuild([]diff.FileEntry{{Path: "a.go"}, {Path: "b.go"}, {Path: "c.go"}})
	model, _ := m.Update(fileLoadedMsg{file: "a.go", lines: files["a.go"]})
	m = model.(Model)
	m.layout.focus = paneTree
	return m
}

func treeSearchKey(t *testing.T, m Model, key string) Model {
	t.Helper()
	model, cmd := m.Update(tea.KeyPressMsg{Text: key})
	m = model.(Model)
	if cmd != nil {
		msg := cmd()
		require.IsType(t, treeScanMsg{}, msg)
		model, _ = m.Update(msg)
		m = model.(Model)
	}
	return m
}

func TestModel_TreeSearchMatchesContentsNotFilenames(t *testing.T) {
	files := map[string][]diff.DiffLine{
		"needle.go": {{NewNum: 1, Content: "no matching content", ChangeType: diff.ChangeAdd}},
		"one.go":    {{NewNum: 1, Content: "needle first", ChangeType: diff.ChangeAdd}},
		"two.go":    {{NewNum: 1, Content: "needle second", ChangeType: diff.ChangeAdd}},
	}
	m := testModel([]string{"needle.go", "one.go", "two.go"}, files)
	m.tree.Rebuild([]diff.FileEntry{{Path: "needle.go"}, {Path: "one.go"}, {Path: "two.go"}})
	m.tree.SelectByPath("needle.go")
	model, _ := m.Update(fileLoadedMsg{file: "needle.go", lines: files["needle.go"]})
	m = model.(Model)
	m.layout.focus = paneTree
	model, _ = m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = model.(Model)
	require.True(t, m.search.active)
	model, _ = m.Update(tea.KeyPressMsg{Text: "needle"})
	m = model.(Model)
	model, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = model.(Model)
	require.NotNil(t, cmd)
	model, _ = m.Update(cmd())
	m = model.(Model)
	require.Equal(t, "one.go", m.file.name)
	require.Equal(t, "needle first", m.file.lines[m.nav.diffCursor].Content)
	for _, path := range []string{"two.go", "one.go"} {
		m = treeSearchKey(t, m, "n")
		require.Equal(t, path, m.file.name, "skip filename-only matches, including on wrap")
		require.Equal(t, paneTree, m.layout.focus)
	}
}

func TestModel_FocusedSearchFindsContextLines(t *testing.T) {
	for _, focus := range []pane{paneTree, paneDiff} {
		m := treeSearchModel(t)
		m.layout.focus = focus
		m.modes.compact, m.compact.applicable, m.modes.compactContext = true, true, 3
		m.file.lines = []diff.DiffLine{
			{Content: "omitted context", ChangeType: diff.ChangeDivider},
			{NewNum: 100, Content: "changed line", ChangeType: diff.ChangeAdd},
		}
		m.nav.diffCursor = 1
		m.diffRenderer.(*mocks.RendererMock).FileDiffFunc = func(req diff.FileDiffRequest) ([]diff.DiffLine, error) {
			require.Zero(t, req.ContextLines, "search must request full context for every file")
			if req.Path != "a.go" {
				return []diff.DiffLine{{NewNum: 1, Content: "TODO another file", ChangeType: diff.ChangeContext}}, nil
			}
			return []diff.DiffLine{
				{NewNum: 10, Content: "TODO earlier", ChangeType: diff.ChangeContext},
				{NewNum: 90, Content: "ordinary context", ChangeType: diff.ChangeContext},
				{NewNum: 100, Content: "changed line", ChangeType: diff.ChangeAdd},
				{NewNum: 110, Content: "TODO later", ChangeType: diff.ChangeContext},
			}, nil
		}
		m.startSearch()
		m.search.input.SetValue("TODO")
		cmd := m.submitSearch()
		require.NotNil(t, cmd)
		require.True(t, m.livePaused())
		queue := []tea.Cmd{cmd}
		for len(queue) > 0 {
			cmd, queue = queue[0], queue[1:]
			if cmd == nil {
				continue
			}
			msg := cmd()
			if batch, ok := msg.(tea.BatchMsg); ok {
				queue = append(queue, batch...)
				continue
			}
			model, next := m.Update(msg)
			m = model.(Model)
			queue = append(queue, next)
		}
		require.False(t, m.modes.compact)
		require.Equal(t, focus, m.layout.focus)
		require.Equal(t, 110, m.file.lines[m.nav.diffCursor].NewNum, "search starts at the original source line, not its compact row")
		m = treeSearchKey(t, m, "n")
		if focus == paneTree {
			require.Equal(t, "b.go", m.file.name)
		} else {
			require.Equal(t, "a.go", m.file.name)
			require.Equal(t, 10, m.file.lines[m.nav.diffCursor].NewNum)
		}
	}
}

func TestModel_FocusedSearchDoesNotResumeAfterCancel(t *testing.T) {
	m := treeSearchModel(t)
	m.modes.compact, m.compact.applicable, m.modes.compactContext = true, true, 3
	m.startSearch()
	m.search.input.SetValue("needle")
	cmd := m.submitSearch()
	require.NotNil(t, cmd)
	msg := cmd()
	m.clearSearch()
	model, _ := m.Update(msg)
	m = model.(Model)
	require.Empty(t, m.search.term)
	require.Equal(t, treeScanIdle, m.nav.scanKind)
	require.Equal(t, "a.go", m.file.name)
}

func TestModel_SearchScopeFollowsFocus(t *testing.T) {
	m := treeSearchModel(t)
	require.True(t, m.tree.SelectByVisibleRow(0)) // root directory, not a file
	require.NotEqual(t, "a.go", m.tree.SelectedFile())
	m.startSearch()
	m.search.input.SetValue("needle")
	cmd := m.submitSearch()
	require.NotNil(t, cmd)
	require.Equal(t, "searching…", m.searchSegment())
	require.True(t, m.livePaused(), "refresh cannot overwrite a running search")
	model, _ := m.Update(cmd())
	m = model.(Model)
	require.Equal(t, 0, m.nav.diffCursor)
	require.Equal(t, "a.go", m.tree.SelectedFile(), "select the match even when its file is already loaded")

	for _, step := range []struct {
		focus     pane
		key, file string
		line      int
	}{
		{paneTree, "n", "a.go", 2},
		{paneTree, "n", "c.go", 1}, // skips the file with no matches
		{paneDiff, "n", "c.go", 1}, // wraps within this file, not the tree
		{paneDiff, "N", "c.go", 1},
		{paneTree, "n", "a.go", 0}, // wraps across files
		{paneTree, "N", "c.go", 1}, // reverse wrap
		{paneTree, "N", "a.go", 2},
		{paneDiff, "n", "a.go", 0},
		{paneDiff, "N", "a.go", 2},
	} {
		m.layout.focus = step.focus
		m = treeSearchKey(t, m, step.key)
		require.Equal(t, step.file, m.file.name)
		require.Equal(t, step.line, m.nav.diffCursor)
		require.Equal(t, step.focus, m.layout.focus, "search must not steal focus from the tree")
		require.Equal(t, step.file, m.tree.SelectedFile())
		require.True(t, m.buildSearchMatchSet()[step.line])
		require.Equal(t, "needle", m.search.term)
	}
}

func TestModel_TreeFocusFindsMatchAbsentFromCurrentFile(t *testing.T) {
	m := treeSearchModel(t)
	m.layout.focus = paneDiff
	m.startSearch()
	m.search.input.SetValue("third") // absent locally, present in c.go
	require.Nil(t, m.submitSearch(), "file search does not fetch other files")
	m = treeSearchKey(t, m, "n")
	require.Equal(t, "a.go", m.file.name)
	require.Empty(t, m.search.matches)
	m.layout.focus = paneTree
	m = treeSearchKey(t, m, "n")
	require.Equal(t, "c.go", m.file.name)
	require.Equal(t, 1, m.nav.diffCursor)

	m.startCommand()
	m.command.input.SetValue("search")
	model, _ := m.submitCommand()
	m = model.(Model)
	require.True(t, m.search.active, ":search works from the tree too")
	m.search.input.SetValue("first")
	cmd := m.submitSearch()
	require.NotNil(t, cmd)
	model, _ = m.Update(cmd())
	m = model.(Model)
	require.Equal(t, "a.go", m.file.name)
	require.Equal(t, 0, m.nav.diffCursor)
}

func TestModel_TreeSearchFiltersNoMatchesAndErrors(t *testing.T) {
	m := treeSearchModel(t)
	m.tree.ToggleFilter(map[string]bool{"a.go": true, "b.go": true})
	m.search.term = "needle"
	m.refreshSearchMatches()
	m.nav.diffCursor = 2
	m = treeSearchKey(t, m, "n")
	require.Equal(t, "a.go", m.file.name, "filtered-out c.go must not be searched")
	require.Equal(t, 0, m.nav.diffCursor)

	m.search.term = "absent"
	m.refreshSearchMatches()
	for _, key := range []string{"n", "N"} {
		m = treeSearchKey(t, m, key)
		require.Equal(t, "No matches in file tree", m.output.hint)
		require.Equal(t, "a.go", m.file.name)
		require.Equal(t, 0, m.nav.diffCursor)
	}
	m.diffRenderer.(*mocks.RendererMock).FileDiffFunc = func(req diff.FileDiffRequest) ([]diff.DiffLine, error) {
		return nil, fmt.Errorf("cannot read %s", req.Path)
	}
	m = treeSearchKey(t, m, "n")
	require.Contains(t, m.output.hint, "Search failed:")
	require.Contains(t, m.output.hint, "cannot read b.go")
	require.Equal(t, "a.go", m.file.name)
}

func TestModel_TreeSearchDiscardsSupersededResults(t *testing.T) {
	for _, change := range []string{"clear", "new query", "command", "cursor", "reload", "navigation", "focus", "filter"} {
		t.Run(change, func(t *testing.T) {
			m := treeSearchModel(t)
			m.search.term = "third"
			cmd := m.scanTree(treeScanSearch, true, true)
			require.NotNil(t, cmd)
			require.Nil(t, m.scanTree(treeScanSearch, true, false), "only one scan may run at a time")
			msg := cmd()
			switch change {
			case "clear":
				model, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
				m = model.(Model)
			case "new query":
				m.startSearch()
				m.search.input.SetValue("first")
				m.submitSearch()
			case "command":
				m.startCommand()
				m.closeCommand()
			case "cursor":
				m.nav.diffCursor = 2
			case "reload":
				m.triggerReload()
			case "navigation":
				m.requestFileDiff("b.go")
			case "focus":
				m.layout.focus = paneDiff
			case "filter":
				m.tree.ToggleFilter(map[string]bool{"a.go": true})
			}
			cursor := m.nav.diffCursor
			model, _ := m.Update(msg)
			m = model.(Model)
			require.Equal(t, "a.go", m.file.name)
			require.Equal(t, cursor, m.nav.diffCursor)
		})
	}
}

func TestModel_TreeSearchUsesEffectiveDiffAndSkipsHiddenLines(t *testing.T) {
	m := treeSearchModel(t)
	m.tree.Rebuild([]diff.FileEntry{{Path: "a.go"}, {Path: "renamed.go", OldPath: "old.go", Status: diff.FileAdded}})
	m.search.term = "target"
	m.modes.compact, m.compact.applicable, m.modes.compactContext = true, true, 3
	m.modes.collapsed.enabled = true
	var requests []diff.FileDiffRequest
	m.diffRenderer.(*mocks.RendererMock).FileDiffFunc = func(req diff.FileDiffRequest) ([]diff.DiffLine, error) {
		requests = append(requests, req)
		if !req.Staged {
			return nil, nil // staged-only added file fallback
		}
		return []diff.DiffLine{
			{OldNum: 1, Content: "target hidden", ChangeType: diff.ChangeRemove},
			{NewNum: 1, Content: "replacement", ChangeType: diff.ChangeAdd},
			{Content: "target divider", ChangeType: diff.ChangeDivider},
			{NewNum: 8, Content: "target visible", ChangeType: diff.ChangeContext},
		}, nil
	}
	m = treeSearchKey(t, m, "n")
	require.Equal(t, "renamed.go", m.file.name)
	require.Equal(t, "old.go", m.file.oldName)
	require.Equal(t, 3, m.nav.diffCursor)
	require.Len(t, requests, 2)
	for _, req := range requests {
		require.Equal(t, "renamed.go", req.Path)
		require.Equal(t, "old.go", req.OldPath)
		require.Equal(t, 3, req.ContextLines)
	}
	require.False(t, requests[0].Staged)
	require.True(t, requests[1].Staged)
}
