package keymap

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testCatalog []CommandInfo

func (c testCatalog) Commands() []CommandInfo  { return c }
func (c testCatalog) DumpNotes() []HelpSection { return nil }

func TestReferenceSharesCommandsAndAliases(t *testing.T) {
	km := Default()
	km.Bind("alt+h", Command("h"))
	km.Bind("alt+s", Command("send"))
	km.Bind("alt+g", Command(`! git log --format="%h  %s"`))
	km.Bind("alt+j", Command(`! git log --format="%h  %s"`))
	catalog := testCatalog{
		{Name: "help", Aliases: []string{"h"}, Action: ActionHelp, Section: "Miscellaneous", Description: "show help"},
		{Name: "send", Section: "Harness", Description: "send a message"},
	}
	ref := km.Reference(catalog)
	entries := make(map[string]HelpEntryWithKeys)
	for _, section := range ref.HelpSections() {
		for _, entry := range section.Entries {
			if entry.Command == "" {
				continue
			}
			require.NotContains(t, entries, entry.Command, "aliases must share their command's row")
			entries[entry.Command] = entry
		}
	}
	assert.Equal(t, "? / alt+h", entries[":help (:h)"].Keys)
	assert.Equal(t, "alt+s", entries[":send"].Keys)
	assert.Equal(t, "alt+g / alt+j", entries[`:! git log --format="%h  %s"`].Keys)
	var dump strings.Builder
	require.NoError(t, ref.Dump(&dump))
	for _, command := range []string{":help (:h)", ":send"} {
		assert.Contains(t, dump.String(), "# "+command+" — "+entries[command].Description)
	}
	assert.Contains(t, dump.String(), "map alt+h :h\n")
	require.ErrorContains(t, ref.Dump(&failWriter{}), "write error")
}
