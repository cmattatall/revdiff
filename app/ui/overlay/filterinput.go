package overlay

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/umputun/revdiff/app/ui/style"
)

// filterInput gives popup filters the same editor as annotations and search.
// Commands carry an identity so late clipboard reads cannot edit a reopened popup.
type filterInput struct {
	textinput.Model
	identity *int
}

type filterInputMsg struct {
	identity *int
	msg      tea.Msg
}

func (f *filterInput) open() {
	f.Model = textinput.New()
	f.identity = new(int)
	f.Prompt = ""
	f.Placeholder = "type to filter..."
	styles := f.Styles()
	styles.Cursor.Blink = false
	f.SetStyles(styles)
	f.Focus()
}

// handleKey reserves editing keys and printable input before list keybindings.
// Up/Down and Enter/Esc remain the responsibility of the enclosing popup.
func (f *filterInput) handleKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	// Terminals can encode Backspace as either DEL or Ctrl+H.
	if msg.String() == "ctrl+alt+h" {
		msg = tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt}
	}
	k := f.KeyMap
	if msg.Text != "" || key.Matches(msg,
		k.CharacterForward, k.CharacterBackward, k.WordForward, k.WordBackward,
		k.DeleteWordBackward, k.DeleteWordForward, k.DeleteAfterCursor, k.DeleteBeforeCursor,
		k.DeleteCharacterBackward, k.DeleteCharacterForward, k.LineStart, k.LineEnd, k.Paste) {
		var cmd tea.Cmd
		f.Model, cmd = f.Model.Update(msg)
		if cmd != nil {
			identity := f.identity
			return true, func() tea.Msg { return filterInputMsg{identity: identity, msg: cmd()} }
		}
		return true, nil
	}
	return false, nil
}

func (f *filterInput) render(width int, resolver Resolver) string {
	f.SetWidth(max(width-2, 1))
	styles := f.Styles()
	styles.Focused.Text = resolver.Style(style.StyleKeyAnnotInputText)
	styles.Focused.Placeholder = resolver.Style(style.StyleKeyAnnotInputPlaceholder)
	styles.Cursor.Color = resolver.Style(style.StyleKeyAnnotInputCursor).GetForeground()
	styles.Blurred = styles.Focused
	f.SetStyles(styles)
	return "  " + f.View()
}
