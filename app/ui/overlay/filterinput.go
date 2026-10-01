package overlay

import (
	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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
	f.Cursor.SetMode(cursor.CursorStatic)
	f.Focus()
}

// handleKey reserves editing keys and printable input before list keybindings.
// Up/Down and Enter/Esc remain the responsibility of the enclosing popup.
func (f *filterInput) handleKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	k := f.KeyMap
	if (msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace) && !msg.Alt || key.Matches(msg,
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
	f.Width = max(width-2, 1)
	f.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(resolver.Color(style.ColorKeyNormalFg)))
	f.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(resolver.Color(style.ColorKeyMutedFg)))
	f.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(resolver.Color(style.ColorKeyAccentFg)))
	f.Cursor.TextStyle = f.TextStyle
	return "  " + f.View()
}
