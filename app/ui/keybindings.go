package ui

import "github.com/umputun/revdiff/app/keymap"

func (e commandEntry) reference() keymap.CommandInfo {
	return keymap.CommandInfo{Name: e.name, Aliases: e.aliases, Description: e.description, Section: e.section}
}

// Commands exposes registry metadata to the keymap's help and dump interface.
func (m Model) Commands() []keymap.CommandInfo {
	var entries []keymap.CommandInfo
	for _, command := range m.commandEntries() {
		entry := command.commandEntry.reference()
		if command.action != "" && m.paletteCommand(command.action) == entry.Name {
			entry.Action = command.action
		}
		entries = append(entries, entry)
	}
	return entries
}

// DumpNotes documents fixed input controls separately from configurable bindings.
func (m Model) DumpNotes() []keymap.HelpSection {
	input := keymap.HelpSection{Name: "Input controls (take precedence over review bindings)", Entries: []keymap.HelpEntryWithKeys{
		{Keys: ":<line> / :$", Description: "go to source line / last source line"},
		{Keys: ":! <command> / :!!", Description: "run / repeat a shell command"},
		{Keys: "Enter / Esc / Tab / Ctrl+R", Description: "submit / cancel / complete / search command history"},
	}}
	vim := keymap.HelpSection{Name: "Optional --vim-motion preset (fixed bindings, handled before review bindings)"}
	for _, entry := range m.buildVimMotionHelpSection().Entries {
		vim.Entries = append(vim.Entries, keymap.HelpEntryWithKeys{Keys: entry.Keys, Description: entry.Description})
	}
	return []keymap.HelpSection{input, vim}
}

type commandReference struct {
	entries []keymap.CommandInfo
	notes   []keymap.HelpSection
}

func (r commandReference) Commands() []keymap.CommandInfo  { return r.entries }
func (r commandReference) DumpNotes() []keymap.HelpSection { return r.notes }

// CommandReference includes commands from all review contexts without loading files
// or executing providers. Only command metadata is read from these model states.
func CommandReference(cfg ModelConfig) keymap.CommandCatalog {
	m := Model{
		keymap:     keymap.Default(),
		tree:       &workingTree{},
		live:       liveState{harnesses: cfg.Harnesses},
		inspection: inspectionState{installCommands: cfg.LSPInstallCommands},
	}
	entries := m.Commands()
	m.cfg.workingTree, m.file.staged, m.layout.focus = true, true, paneDiff
	return commandReference{entries: append(entries, m.Commands()...), notes: m.DumpNotes()}
}
