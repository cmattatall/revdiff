package overlay

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilterInputCursorEditing(t *testing.T) {
	var input filterInput
	input.open()
	input.SetValue("αβ γδ")
	input.SetCursor(3)
	for _, msg := range []tea.KeyMsg{
		{Type: tea.KeyCtrlA},
		{Type: tea.KeyRunes, Runes: []rune("X")},
		{Type: tea.KeyCtrlE},
		{Type: tea.KeyBackspace},
	} {
		handled, _ := input.handleKey(msg)
		require.True(t, handled)
	}
	assert.Equal(t, "Xαβ γ", input.Value())
	assert.Equal(t, 5, input.Position())
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
		msg := filterInputMsg{identity: input.identity, msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)}}
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
