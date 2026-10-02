package keymap

import (
	"fmt"
	"io"
	"slices"
	"strings"
)

// CommandCatalog supplies metadata without executing commands or loading a review.
type CommandCatalog interface {
	Commands() []CommandInfo
	DumpNotes() []HelpSection
}

// CommandInfo describes a palette command and its optional equivalent action.
type CommandInfo struct {
	Name, Description, Section string
	Aliases                    []string
	Action                     Action
}

func (c CommandInfo) matches(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	return c.Name == text || slices.Contains(c.Aliases, text)
}

// HelpName keeps aliases on the same row as their canonical command.
func (c CommandInfo) HelpName() string {
	name := ":" + c.Name
	if len(c.Aliases) > 0 {
		name += " (:" + strings.Join(c.Aliases, ", :") + ")"
	}
	return name
}

// Reference joins effective bindings to a command catalog for help and export.
type Reference struct {
	keymap   *Keymap
	commands []CommandInfo
	notes    []HelpSection
}

func (km *Keymap) Reference(catalog CommandCatalog) Reference {
	return Reference{keymap: km, commands: catalog.Commands(), notes: catalog.DumpNotes()}
}

// HelpSections includes bound actions, all catalog commands, and custom targets.
// Keys remain in their configuration spelling for the UI to format.
func (r Reference) HelpSections() []HelpSection {
	sections := r.keymap.HelpSections()
	commandKeys := make(map[string][]string)
	var custom []string
	for _, binding := range r.keymap.commandBindings() {
		text := string(binding.target.(Command))
		name := ":" + text
		known := false
		for _, command := range r.commands {
			if command.matches(text) {
				name, known = command.Name, true
				break
			}
		}
		if !known && len(commandKeys[name]) == 0 {
			custom = append(custom, name)
		}
		commandKeys[name] = append(commandKeys[name], binding.key)
	}
	shown := make(map[string]bool)
	for i := range sections {
		for j := range sections[i].Entries {
			entry := &sections[i].Entries[j]
			for _, command := range r.commands {
				if command.Action != entry.Action {
					continue
				}
				entry.Command = command.HelpName()
				if extra := strings.Join(commandKeys[command.Name], " / "); extra != "" {
					entry.Keys += " / " + extra
				}
				shown[command.Name] = true
				break
			}
		}
	}
	appendEntry := func(section string, entry HelpEntryWithKeys) {
		if section == "" {
			section = "Miscellaneous"
		}
		for i := range sections {
			if sections[i].Name == section {
				sections[i].Entries = append(sections[i].Entries, entry)
				return
			}
		}
		sections = append(sections, HelpSection{Name: section, Entries: []HelpEntryWithKeys{entry}})
	}
	for _, command := range r.commands {
		if !shown[command.Name] {
			appendEntry(command.Section, HelpEntryWithKeys{Command: command.HelpName(),
				Keys: strings.Join(commandKeys[command.Name], " / "), Description: command.Description})
			shown[command.Name] = true
		}
	}
	for _, name := range custom {
		appendEntry("Custom bindings", HelpEntryWithKeys{Command: name,
			Keys: strings.Join(commandKeys[name], " / "), Description: "run configured command"})
	}
	return sections
}

// Dump writes reloadable bindings and command documentation from the same catalog.
func (r Reference) Dump(w io.Writer) error {
	var out strings.Builder
	if err := r.keymap.Dump(&out); err != nil {
		return err
	}
	commands := slices.Clone(r.commands)
	slices.SortFunc(commands, func(a, b CommandInfo) int {
		return strings.Compare(a.Section+"\x00"+a.Name, b.Section+"\x00"+b.Name)
	})
	out.WriteString("\n# Command reference\n# Commands and aliases can be bound with map <key> :<command>.\n")
	out.WriteString("# Availability depends on the current review and selection.\n")
	seen := make(map[string]bool)
	section := ""
	for _, command := range commands {
		if seen[command.Name] {
			continue
		}
		seen[command.Name] = true
		if section != command.Section {
			section = command.Section
			fmt.Fprintf(&out, "\n# Commands: %s\n", section)
		}
		fmt.Fprintf(&out, "# %s — %s\n# map <key> :%s\n", command.HelpName(), command.Description, command.Name)
	}
	for _, note := range r.notes {
		fmt.Fprintf(&out, "\n# %s\n", note.Name)
		for _, entry := range note.Entries {
			fmt.Fprintf(&out, "# %s — %s\n", entry.Keys, entry.Description)
		}
	}
	if _, err := io.WriteString(w, out.String()); err != nil {
		return fmt.Errorf("dump keybindings: %w", err)
	}
	return nil
}
