package ui

import (
	"sync"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/umputun/revdiff/app/diff"
)

// Share the worker across Model snapshots. At most one file is tokenized at a
// time, and queued commands skip files the user has already navigated past.
type highlightWork struct {
	mu  sync.Mutex
	seq atomic.Uint64
}

type highlightedMsg struct {
	seq   uint64
	style string
	lines []string
}

func (m *Model) loadHighlight() tea.Cmd {
	work := m.highlightWork
	seq := work.seq.Add(1)
	m.file.highlighted = nil
	if len(m.file.lines) == 0 {
		return nil
	}
	if cache, ok := m.highlighter.(interface {
		CachedLines(string, []diff.DiffLine) ([]string, bool)
	}); ok {
		if lines, hit := cache.CachedLines(m.file.name, m.file.lines); hit {
			m.file.highlighted = lines
			return nil
		}
	}
	highlighter, file, lines := m.highlighter, m.file.name, m.file.lines
	style := highlighter.StyleName()
	return func() tea.Msg {
		work.mu.Lock()
		defer work.mu.Unlock()
		if seq != work.seq.Load() || style != highlighter.StyleName() {
			return nil
		}
		return highlightedMsg{seq: seq, style: style, lines: highlighter.HighlightLines(file, lines)}
	}
}

func (m Model) handleHighlighted(msg highlightedMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.highlightWork.seq.Load() || msg.style != m.highlighter.StyleName() || len(msg.lines) == 0 {
		return m, nil
	}
	m.file.highlighted = msg.lines
	m.invalidateRenderCaches()
	offset := m.layout.viewport.YOffset
	m.layout.viewport.SetContent(m.renderDiff())
	m.layout.viewport.SetYOffset(offset)
	return m, nil
}
