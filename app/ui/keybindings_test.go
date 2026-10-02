package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/revdiff/app/keymap"
	"github.com/umputun/revdiff/app/ui/overlay"
)

func TestDumpKeys(t *testing.T) {
	km := keymap.Default()
	km.Unbind("j")
	km.Bind("alt+s", keymap.Command("hs"))
	cfg := ModelConfig{
		Keymap: km,
		Harnesses: map[string]func() ([]FeedbackSender, error){
			"example": func() ([]FeedbackSender, error) { panic("dump must not connect") },
		},
		LSPInstallCommands: map[string]string{"example": "must-not-run"},
	}
	var out strings.Builder
	require.NoError(t, km.Reference(CommandReference(cfg)).Dump(&out))
	for _, text := range []string{
		"# open focused file in $EDITOR", "# map <key> open_file_in_editor",
		"# :focus changed (:fc)", "# :focus diff (:fd)", "# :harness send (:hs)",
		"# :harness connect example", "# :lsp install example",
		"# :stage file", "# :unstage file", "# :files untracked show",
		"# map <key> :harness send", "unmap j\n", "map alt+s :hs\n",
	} {
		assert.Contains(t, out.String(), text)
	}
	assert.Equal(t, 1, strings.Count(out.String(), "# :harness send (:hs)"))
	var again strings.Builder
	require.NoError(t, km.Reference(CommandReference(cfg)).Dump(&again))
	assert.Equal(t, out.String(), again.String(), "dump order must be stable")
	path := filepath.Join(t.TempDir(), "keys")
	require.NoError(t, os.WriteFile(path, []byte(out.String()), 0o600))
	loaded, err := keymap.Load(path)
	require.NoError(t, err)
	assert.Empty(t, loaded.Resolve("j"))
	assert.Equal(t, keymap.Command("hs"), loaded.ResolveTarget("alt+s"))
	assert.Equal(t, keymap.ActionUp, loaded.Resolve("k"))
}

func TestCommandBindings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys")
	require.NoError(t, os.WriteFile(path, []byte("map x :set number\nmap ctrl+w>d :fd\nmap alt+z :not-a-command\n"), 0o600))
	km, err := keymap.Load(path)
	require.NoError(t, err)
	m := splitTestModel(t)
	m.keymap = km
	m.modes.lineNumbers = false
	for range 2 {
		model, _ := m.Update(tea.KeyPressMsg{Text: "x"})
		m = model.(Model)
		assert.True(t, m.modes.lineNumbers, "an explicit setting must not toggle on repeated use")
		assert.False(t, m.command.active)
	}
	for _, key := range []tea.KeyPressMsg{{Code: 'w', Mod: tea.ModCtrl}, {Text: "d"}} {
		model, _ := m.Update(key)
		m = model.(Model)
	}
	assert.Equal(t, paneDiff, m.layout.focus, "chords must execute command aliases")
	m.startCommand()
	model, _ := m.Update(tea.KeyPressMsg{Text: "x"})
	m = model.(Model)
	assert.Equal(t, "x", m.command.input.Value(), "typing must not execute a review binding")
	m.closeCommand()
	model, _ = m.Update(tea.KeyPressMsg{Code: 'z', Mod: tea.ModAlt})
	m = model.(Model)
	assert.True(t, m.command.active)
	assert.Equal(t, "Unknown command", m.command.err)
	assert.Equal(t, "not-a-command", m.command.input.Value())
}

func TestHelpCommandBindings(t *testing.T) {
	m := splitTestModel(t)
	m.keymap.Bind("alt+s", keymap.Command("hs"))
	m.keymap.Bind("alt+d", keymap.Command("focus diff"))
	m.keymap.Bind("alt+g", keymap.Command("! git status"))
	m.keymap.Bind("alt+j", keymap.Command("! git status"))
	entries := map[string]overlay.HelpEntry{}
	for _, section := range m.buildHelpSpec().Sections {
		for _, entry := range section.Entries {
			if entry.Command != "" {
				require.NotContains(t, entries, entry.Command)
				entries[entry.Command] = entry
			}
		}
	}
	assert.Equal(t, "alt+s", entries[":harness send (:hs)"].Keys)
	assert.Equal(t, "l / alt+d", entries[":focus diff (:fd)"].Keys)
	assert.Equal(t, "alt+g / alt+j", entries[":! git status"].Keys)
}
