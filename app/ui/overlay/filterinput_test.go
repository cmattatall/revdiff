package overlay

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilterInputCursorEditing(t *testing.T) {
	var input filterInput
	input.open()
	input.SetValue("αβ γδ")
	input.SetCursor(3)
	for _, msg := range []tea.KeyPressMsg{
		{Code: 'a', Mod: tea.ModCtrl},
		{Text: "X"},
		{Code: 'e', Mod: tea.ModCtrl},
		{Code: tea.KeyBackspace},
	} {
		handled, _ := input.handleKey(msg)
		require.True(t, handled)
	}
	assert.Equal(t, "Xαβ γ", input.Value())
	assert.Equal(t, 5, input.Position())
}

func TestFilterInputBracketedPaste(t *testing.T) {
	for _, kind := range []Kind{KindFilePicker, KindThemeSelect, KindInspection} {
		mgr := NewManager()
		switch kind {
		case KindFilePicker:
			mgr.OpenFilePicker(FilePickerSpec{Paths: []string{"alpha.go", "beta.go"}})
			mgr.HandleInput(tea.PasteMsg{Content: "beta"})
			assert.Equal(t, []string{"beta.go"}, mgr.filePick.entries)
		case KindThemeSelect:
			mgr.OpenThemeSelect(themeSpec())
			out := mgr.HandleInput(tea.PasteMsg{Content: "drac"})
			require.Equal(t, OutcomeThemePreview, out.Kind)
			assert.Equal(t, "dracula", out.ThemeChoice.Name)
		case KindInspection:
			mgr.OpenInspection(InspectionSpec{Items: []string{"alpha", "beta"}})
			mgr.HandleInput(tea.PasteMsg{Content: "beta"})
			assert.Equal(t, []string{"beta"}, mgr.inspect.picker.entries)
		default:
			t.Fatalf("unsupported filter popup: %v", kind)
		}
		assert.True(t, mgr.Active(), "pasting edits the filter without executing a shortcut")
	}
}

func TestFilterInputAsyncDelivery(t *testing.T) {
	for _, theme := range []bool{false, true} {
		mgr := NewManager()
		open := func() *filterInput {
			if theme {
				mgr.OpenThemeSelect(themeSpec())
				return &mgr.themeSel.filter
			}
			mgr.OpenFilePicker(filePickerSpec())
			return &mgr.filePick.filter
		}
		input := open()
		text := "view"
		if theme {
			text = "drac"
		}
		// Use a deterministic textinput message rather than accessing the OS clipboard.
		msg := filterInputMsg{identity: input.identity, msg: tea.KeyPressMsg{Text: text}}
		out := mgr.HandleInput(msg)
		assert.Equal(t, text, input.Value())
		if theme {
			require.Equal(t, OutcomeThemePreview, out.Kind)
			assert.Equal(t, "dracula", out.ThemeChoice.Name)
		} else {
			assert.Equal(t, []string{"app/ui/view.go"}, mgr.filePick.entries)
		}
		mgr.Close()
		input = open()
		mgr.HandleInput(msg)
		assert.Empty(t, input.Value(), "late result must not edit a reopened popup")
	}
}
