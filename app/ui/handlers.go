package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/umputun/revdiff/app/diff"
	"github.com/umputun/revdiff/app/keymap"
	"github.com/umputun/revdiff/app/ui/overlay"
	"github.com/umputun/revdiff/app/ui/sidepane"
)

// helpKeyDisplay maps bubbletea key names to user-friendly display names.
var helpKeyDisplay = map[string]string{
	"pgdown": "PgDn",
	"pgup":   "PgUp",
	"left":   "←",
	"right":  "→",
	"home":   "Home",
	"end":    "End",
	"enter":  "Enter",
	"esc":    "Esc",
	"tab":    "Tab",
	"up":     "↑",
	"down":   "↓",
	" ":      "Space",
}

// displayKeyName returns a user-friendly display name for a bubbletea key.
func (m Model) displayKeyName(key string) string {
	if d, ok := helpKeyDisplay[key]; ok {
		return d
	}
	if strings.HasPrefix(key, "ctrl+") {
		suffix := key[5:]
		if suffix != "" {
			return "Ctrl+" + strings.ToUpper(suffix[:1]) + suffix[1:]
		}
		return "Ctrl+"
	}
	return key
}

// formatKeysForHelp returns a formatted key string for a given action using display names.
func (m Model) formatKeysForHelp(action keymap.Action) string {
	keys := m.keymap.KeysFor(action)
	display := make([]string, len(keys))
	for i, k := range keys {
		display[i] = m.displayKeyName(k)
	}
	return strings.Join(display, " / ")
}

// commandHelpName groups aliases with their canonical command.
func (m Model) commandHelpName(name string) string {
	switch name {
	case "", "a", "h", "q", "fd", "fc", "fs", "bv", "harness send":
		return ""
	case "annotate":
		return ":annotate (:a)"
	case "help":
		return ":help (:h)"
	case "quit":
		return ":quit (:q)"
	case "focus diff":
		return ":focus diff (:fd)"
	case "focus changed":
		return ":focus changed (:fc)"
	case "focus staged":
		return ":focus staged (:fs)"
	case "blame view":
		return ":blame view (:bv)"
	case "w":
		return ":w (:harness send)"
	default:
		return ":" + name
	}
}

// buildHelpSpec builds an overlay.HelpSpec from the keymap's help sections,
// converting raw key names to display names and inserting the TOC section.
// When the vim-motion preset is active, appends a synthetic "Vim motion"
// section listing the 11 preset bindings (which have no entries in the base
// keymap since they're only reachable through the interceptor).
func (m Model) buildHelpSpec() overlay.HelpSpec {
	sections := m.keymap.HelpSections()
	var result []overlay.HelpSection
	for _, sec := range sections {
		pad := m.helpIconPad(sec)
		var entries []overlay.HelpEntry
		for _, e := range sec.Entries {
			if m.cfg.workingTree && e.Action == keymap.ActionToggleUntracked {
				continue
			}
			command := m.commandHelpName(m.paletteCommand(e.Action))
			entries = append(entries, overlay.HelpEntry{
				Keys:        m.formatKeysForHelp(e.Action),
				Command:     command,
				Description: m.helpDescriptionWithIcon(e, pad),
			})
		}
		if sec.Name == "Search" {
			entries = append(entries,
				overlay.HelpEntry{Keys: "↑ / Ctrl+P", Description: pad + "recall previous search query (in search prompt)"},
				overlay.HelpEntry{Keys: "↓ / Ctrl+N", Description: pad + "recall next search query / clear (in search prompt)"},
			)
		}
		result = append(result, overlay.HelpSection{Title: sec.Name, Entries: entries})

		if sec.Name == keymap.SectionPane {
			result = append(result, m.buildTOCHelpSection())
		}
	}
	if m.modes.vimMotion {
		result = append(result, m.buildVimMotionHelpSection())
	}
	// Include harness connectors and actions with no key binding.
	palette := overlay.HelpSection{Title: "Command palette"}
	for _, entry := range m.commandEntries() {
		name := m.commandHelpName(entry.name)
		if name == "" {
			continue
		}
		if entry.name == m.paletteCommand(entry.action) && len(m.keymap.KeysFor(entry.action)) > 0 {
			continue
		}
		palette.Entries = append(palette.Entries, overlay.HelpEntry{
			Command: name, Description: entry.description,
		})
	}
	if m.shell != nil {
		palette.Entries = append(palette.Entries, overlay.HelpEntry{Command: ":! <command>", Description: "run a shell command"})
		palette.Entries = append(palette.Entries, overlay.HelpEntry{Command: ":!!", Description: "repeat the previous shell command"})
	}
	result = append(result, palette)
	return overlay.HelpSpec{Sections: result}
}

// helpIconPad returns the indent that keeps a section's description column straight
// once some of its rows carry a glyph; a section with no glyph rows gets none.
func (m Model) helpIconPad(sec keymap.HelpSection) string {
	for _, e := range sec.Entries {
		if _, ok := statusIconForAction[e.Action]; ok {
			return "  "
		}
	}
	return ""
}

func (m Model) helpDescriptionWithIcon(e keymap.HelpEntryWithKeys, pad string) string {
	if icon, ok := statusIconForAction[e.Action]; ok {
		return icon + " " + e.Description
	}
	return pad + e.Description
}

// buildVimMotionHelpSection returns the synthetic help section for the
// vim-motion preset. Keys are hardcoded because these bindings are not part
// of the configurable keymap — they are driven by the interceptor in
// vimmotion.go, not by defaultBindings.
func (m Model) buildVimMotionHelpSection() overlay.HelpSection {
	return overlay.HelpSection{
		Title: "Vim motion",
		Entries: []overlay.HelpEntry{
			{Keys: "N j / N k", Description: "move cursor N lines down/up"},
			{Keys: "gg", Description: "jump to first line"},
			{Keys: "G / N G", Description: "jump to last line / goto line N"},
			{Keys: "H / N H", Description: "cursor to top of screen / Nth line from top"},
			{Keys: "M", Description: "cursor to middle of screen"},
			{Keys: "L / N L", Description: "cursor to bottom of screen / Nth line from bottom"},
			{Keys: "zz", Description: "center viewport on cursor"},
			{Keys: "zt", Description: "align viewport top"},
			{Keys: "zb", Description: "align viewport bottom"},
			{Keys: "ZZ", Description: "quit"},
		},
	}
}

// buildTOCHelpSection returns the Markdown TOC contextual help section.
func (m Model) buildTOCHelpSection() overlay.HelpSection {
	mergedKeys := func(actions ...keymap.Action) string {
		var all []string
		seen := map[string]bool{}
		for _, a := range actions {
			for _, k := range m.keymap.KeysFor(a) {
				dk := m.displayKeyName(k)
				if !seen[dk] {
					all = append(all, dk)
					seen[dk] = true
				}
			}
		}
		return strings.Join(all, " / ")
	}

	return overlay.HelpSection{
		Title: "Markdown TOC (single-file full-context mode)",
		Entries: []overlay.HelpEntry{
			{Keys: mergedKeys(keymap.ActionTogglePane), Description: "switch between TOC and diff"},
			{Keys: mergedKeys(keymap.ActionDown, keymap.ActionUp), Description: "navigate TOC entries"},
			{Keys: mergedKeys(keymap.ActionNextItem, keymap.ActionPrevItem), Description: "next / prev header"},
			{Keys: mergedKeys(keymap.ActionConfirm), Description: "jump to header in diff"},
		},
	}
}

// handleFileAnnotateKey starts file-level annotation from diff pane only.
func (m Model) handleFileAnnotateKey() (tea.Model, tea.Cmd) {
	if m.layout.focus != paneDiff || m.file.name == "" {
		return m, nil
	}
	cmd := m.startFileAnnotation()
	m.layout.viewport.SetContent(m.renderDiff())
	return m, cmd
}

// handleEscKey clears search first, otherwise returns to the retained tree selection.
func (m Model) handleEscKey() (tea.Model, tea.Cmd) {
	if m.search.term != "" || len(m.search.matches) > 0 || m.nav.scanKind != treeScanIdle {
		m.clearSearch()
		m.layout.viewport.SetContent(m.renderDiff())
		return m, nil
	}
	if m.layout.focus == paneDiff {
		return m.handleSwitchToTree()
	}
	return m, nil
}

// handleEnterKey handles enter key based on current pane focus.
func (m Model) handleEnterKey() (tea.Model, tea.Cmd) {
	switch m.layout.focus {
	case paneTree:
		if m.file.mdTOC != nil {
			if idx, ok := m.file.mdTOC.CurrentLineIdx(); ok {
				// jump to selected header in diff
				m.nav.diffCursor = idx
				m.annot.cursorOnAnnotation = false
				m.file.mdTOC.UpdateActiveSection(m.nav.diffCursor)
				m.layout.focus = paneDiff
				m.topAlignViewportOnCursor()
				return m, nil
			}
		}
		if m.file.name != "" {
			m.layout.focus = paneDiff
		}
		return m, nil
	case paneDiff:
		var cmd tea.Cmd
		if m.cursorOnFileAnnotationLine() {
			cmd = m.startFileAnnotation()
		} else {
			cmd = m.startAnnotation()
		}
		m.layout.viewport.SetContent(m.renderDiff())
		return m, cmd
	}
	return m, nil
}

// handleFilterToggle toggles the annotated files filter.
// no-op in single-file mode (tree pane is hidden).
func (m Model) handleFilterToggle() (tea.Model, tea.Cmd) {
	if m.file.singleFile {
		return m, nil
	}
	annotated := m.annotatedFiles()
	if len(annotated) > 0 || m.tree.FilterActive() {
		m.pendingAnnotJump = nil    // clear pending annotation jump on manual navigation
		m.nav.pendingHunkJump = nil // clear pending hunk jump on manual navigation
		m.tree.ToggleFilter(annotated)
		m.tree.EnsureVisible(m.treePageSize())
		return m.loadSelectedIfChanged()
	}
	return m, nil
}

// handleUnreviewedFilterToggle toggles the sidebar between all files and files
// still awaiting review. It is unavailable in single-file mode.
func (m Model) handleUnreviewedFilterToggle() (tea.Model, tea.Cmd) {
	if m.file.singleFile {
		return m, nil
	}
	m.pendingAnnotJump = nil
	m.nav.pendingHunkJump = nil
	m.tree.ToggleUnreviewedFilter()
	m.tree.EnsureVisible(m.treePageSize())
	return m.loadSelectedIfChanged()
}

// handleMarkReviewed toggles the reviewed state of the focused file.
// tree focus uses the selected row; diff/TOC focus uses the displayed file.
func (m Model) handleMarkReviewed() (tea.Model, tea.Cmd) {
	file := m.file.name
	if m.layout.focus == paneTree && m.file.mdTOC == nil {
		file = m.tree.SelectedFile()
	}
	if file == "" {
		file = m.tree.SelectedFile()
	}
	if file == "" {
		return m, nil
	}
	staged := m.cfg.workingTree && m.selectedTreeStaged()
	tree := m.tree
	if wt, ok := m.tree.(*workingTree); ok {
		if m.layout.focus == paneDiff {
			staged = m.file.staged
		}
		tree = wt.side(staged)
	}
	key := reviewKey(file, staged)
	if tree.IsReviewed(file) {
		tree.Unreview(file)
		delete(m.reviewed.pending, key)
		return m.loadSelectedIfChanged()
	}
	if _, pending := m.reviewed.pending[key]; pending {
		delete(m.reviewed.pending, key)
		return m, nil
	}
	entry := diff.FileEntry{Path: file, OldPath: tree.OldPath(file), Status: tree.FileStatus(file), Staged: staged}
	if file == m.file.name && (!m.cfg.workingTree || staged == m.file.staged) {
		fingerprint := diff.FileFingerprint(entry, m.file.lines)
		m.reviewed.cache[key] = fingerprint
		tree.SetReviewed(file, fingerprint)
		return m.loadSelectedIfChanged()
	}
	if fingerprint := m.reviewed.cache[key]; fingerprint != "" {
		tree.SetReviewed(file, fingerprint)
		return m.loadSelectedIfChanged()
	}

	m.reviewed.loadSeq++
	seq := m.reviewed.loadSeq
	m.reviewed.pending[key] = seq
	return m, m.loadReviewFingerprint(entry, seq)
}

// handleFileOrSearchNav handles next/prev item navigation: navigates search matches when a search
// is active, otherwise navigates files or TOC entries (no-op in single-file mode without TOC).
func (m Model) handleFileOrSearchNav(forward bool) (tea.Model, tea.Cmd) {
	if m.search.term != "" && m.layout.focus == paneTree && m.file.mdTOC == nil {
		cmd := m.scanTree(treeScanSearch, forward, false)
		return m, cmd
	}
	if len(m.search.matches) > 0 {
		if forward {
			m.nextSearchMatch()
		} else {
			m.prevSearchMatch()
		}
		m.syncTOCActiveSection()
		m.layout.viewport.SetContent(m.renderDiff())
		return m, nil
	}
	if m.search.term != "" && m.layout.focus == paneDiff {
		return m, nil // an unmatched query must not turn n into file navigation
	}
	dir := 1
	if !forward {
		dir = -1
	}
	if m.file.singleFile && m.file.mdTOC != nil {
		return m.jumpTOCEntry(dir)
	}
	if !m.file.singleFile {
		m.pendingAnnotJump = nil    // clear pending annotation jump on manual navigation
		m.nav.pendingHunkJump = nil // clear pending hunk jump on manual navigation
		if forward {
			m.tree.StepFile(sidepane.DirectionNext)
		} else {
			m.tree.StepFile(sidepane.DirectionPrev)
		}
		return m.loadSelectedIfChanged()
	}
	return m, nil
}

// annotatedFiles returns a set of files that have annotations.
func (m Model) annotatedFiles() map[string]bool {
	result := make(map[string]bool)
	for _, f := range m.store.Files() {
		result[f] = true
	}
	return result
}
