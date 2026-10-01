package ui

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/umputun/revdiff/app/diff"
)

type treeScanKind uint8

const (
	treeScanIdle treeScanKind = iota
	treeScanSearch
	treeScanHunk
)

type treeScanMsg struct {
	seq, fileSeq, filesSeq uint64
	kind                   treeScanKind
	paths                  []string
	origin                 string
	originLine             int
	entry                  diff.FileEntry
	lines                  []diff.DiffLine
	line                   int // -1 when no target exists
	err                    error
}

// scanTree finds the next search match or hunk in filtered tree order, wrapping
// around once. Only the target file is retained; renderer IO runs in the command.
func (m *Model) scanTree(kind treeScanKind, forward, inclusive bool) tea.Cmd {
	if m.nav.scanKind != treeScanIdle || m.file.requestedPath != "" || !m.filesLoaded {
		return nil
	}
	paths := m.tree.VisibleFiles()
	if len(paths) == 0 {
		return nil
	}
	entries := make([]diff.FileEntry, len(paths))
	if provider, ok := m.tree.(interface{ VisibleEntries() []diff.FileEntry }); ok {
		entries = provider.VisibleEntries()
	} else {
		for i, path := range paths {
			entries[i] = diff.FileEntry{Path: path, OldPath: m.tree.OldPath(path), Status: m.tree.FileStatus(path)}
		}
	}
	origin := max(0, slices.Index(paths, m.tree.SelectedFile()))
	if indexed, ok := m.tree.(interface{ SelectedVisibleIndex() int }); ok {
		origin = indexed.SelectedVisibleIndex()
	}
	m.nav.scanSeq++
	m.nav.scanKind = kind
	// Commands use a snapshot, never the tree's mutable maps or entries.
	snapshot := *m
	snapshot.modes.collapsed.expandedHunks = maps.Clone(m.modes.collapsed.expandedHunks)
	selected := m.tree.SelectedFile()
	return func() tea.Msg {
		msg := treeScanMsg{seq: snapshot.nav.scanSeq, fileSeq: snapshot.file.loadSeq,
			filesSeq: snapshot.filesLoadSeq, kind: kind, paths: paths, origin: selected,
			originLine: snapshot.nav.diffCursor, line: -1}
		step := 1
		if !forward {
			step = -1
		}
		for visit := 0; visit <= len(entries); visit++ {
			entry := entries[(origin+step*visit+len(entries))%len(entries)]
			probe := snapshot
			if entry.Path != snapshot.file.name {
				var err error
				probe.file.lines, err = snapshot.fetchEffectiveFileDiff(entry, snapshot.currentContextLines(), true)
				if err != nil {
					msg.err = err
					return msg
				}
				probe.modes.collapsed.expandedHunks = nil
			}
			start, end := 0, len(probe.file.lines)
			if !forward {
				start, end = len(probe.file.lines)-1, -1
			}
			split := start
			if entry.Path == snapshot.file.name {
				split = snapshot.nav.diffCursor
				if !inclusive {
					split += step
				}
				if forward {
					split = min(max(split, 0), len(probe.file.lines))
				} else {
					split = min(max(split, -1), len(probe.file.lines)-1)
				}
			}
			if visit == 0 {
				start = split
			} else if visit == len(entries) {
				end = split
			}
			hunks := probe.findHunks()
			targets := make(map[int]bool)
			if kind == treeScanHunk {
				for _, hunk := range hunks {
					targets[probe.firstVisibleInHunk(hunk, hunks)] = true
				}
			}
			for i := start; i != end; i += step {
				line := probe.file.lines[i]
				match := targets[i]
				if kind == treeScanSearch {
					match = line.ChangeType != diff.ChangeDivider && !probe.isCollapsedHidden(i, hunks) &&
						strings.Contains(strings.ToLower(line.Content), snapshot.search.term)
				}
				if match {
					msg.entry, msg.lines, msg.line = entry, probe.file.lines, i
					return msg
				}
			}
		}
		return msg
	}
}

func (m Model) handleTreeScan(msg treeScanMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.nav.scanSeq {
		return m, nil
	}
	m.nav.scanKind = treeScanIdle
	if msg.fileSeq != m.file.loadSeq || msg.filesSeq != m.filesLoadSeq ||
		msg.originLine != m.nav.diffCursor ||
		m.layout.focus != paneTree || m.search.active || m.command.active || m.annot.annotating ||
		m.overlay.Active() || msg.origin != m.tree.SelectedFile() || !slices.Equal(msg.paths, m.tree.VisibleFiles()) {
		return m, nil
	}
	if msg.err != nil {
		m.output.hint = fmt.Sprintf("Search failed: %v", msg.err)
		return m, nil
	}
	if msg.line < 0 {
		m.output.hint = "No matches in file tree"
		if msg.kind == treeScanHunk {
			m.output.hint = "No hunks in file tree"
		}
		return m, nil
	}
	if selector, ok := m.tree.(interface{ SelectEntry(diff.FileEntry) bool }); ok {
		selector.SelectEntry(msg.entry)
	} else {
		m.tree.SelectByPath(msg.entry.Path)
	}
	m.tree.EnsureVisible(m.treePageSize())
	var cmd tea.Cmd
	if msg.entry.Path != m.file.name || (m.cfg.workingTree && msg.entry.Staged != m.file.staged) {
		m.pendingAnnotJump = nil
		m.nav.pendingHunkJump = nil
		m.file.loadSeq++
		model, loadCmd := m.handleFileLoaded(fileLoadedMsg{file: msg.entry.Path, oldName: msg.entry.OldPath, staged: msg.entry.Staged,
			seq: m.file.loadSeq, lines: msg.lines})
		m, cmd = model.(Model), loadCmd
	}
	m.nav.diffCursor = msg.line
	m.annot.cursorOnAnnotation = false
	m.realignSearchCursor()
	m.syncTOCActiveSection()
	if msg.kind == treeScanHunk {
		m.centerHunkInViewport()
	} else {
		m.ensureHunkExpanded(msg.line)
		m.centerViewportOnCursor()
	}
	m.layout.viewport.SetContent(m.renderDiff())
	return m, cmd
}
