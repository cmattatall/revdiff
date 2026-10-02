package keymap

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"
)

type terminalProbe struct {
	keys   []tea.KeyPressMsg
	pastes []string
}

func (m terminalProbe) Init() tea.Cmd  { return nil }
func (m terminalProbe) View() tea.View { return tea.NewView("") }
func (m terminalProbe) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch key := msg.(type) {
	case tea.KeyPressMsg:
		if key.String() == "ctrl+c" {
			return m, tea.Quit
		}
		m.keys = append(m.keys, key)
	case tea.PasteMsg:
		m.pastes = append(m.pastes, key.Content)
	}
	return m, nil
}

func TestModifiedEnterThroughTerminalDecoder(t *testing.T) {
	input := "\x1b[13;9u\x1b[13;9:1u\x1b[13;9:2u\x1b[13;9:3u\x1b[13;5u\r\x1b[200~\x1b[13;9u\x1b[201~\x03"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	p := tea.NewProgram(terminalProbe{}, tea.WithContext(ctx), tea.WithInput(strings.NewReader(input)), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler())
	model, err := p.Run()
	require.NoError(t, err)
	keys := model.(terminalProbe).keys
	require.Len(t, keys, 5, "release events and bracketed paste are not key presses")
	for _, key := range keys[:3] {
		require.Equal(t, "super+enter", key.String())
		require.Equal(t, ActionInspectSymbol, Default().Resolve(key.String()))
	}
	require.Equal(t, "ctrl+enter", keys[3].String())
	require.NotEqual(t, ActionInspectSymbol, Default().Resolve(keys[3].String()))
	require.Equal(t, ActionConfirm, Default().Resolve(keys[4].String()))
	require.Equal(t, []string{"\x1b[13;9u"}, model.(terminalProbe).pastes)
	km := Default()
	km.Bind(normalizeKey("Command+Enter"), ActionHelp)
	require.Equal(t, ActionHelp, km.Resolve(keys[0].String()))
}
