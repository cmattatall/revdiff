package ui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/umputun/revdiff/app/diff"
)

// searchHistoryMax bounds the retained per-session search-query history.
// when exceeded, oldest entries are dropped.
const searchHistoryMax = 50

type focusedSearchLoadedMsg struct {
	fileLoadedMsg
	searchSeq uint64
	focus     pane
}

// startSearch creates a search textinput and enters searching mode.
func (m *Model) startSearch() tea.Cmd {
	if !m.filesLoaded || m.file.requestedPath != "" {
		return nil
	}
	m.clearPendingInputState()
	m.nav.scanSeq++
	m.nav.scanKind = treeScanIdle
	ti := textinput.New()
	ti.Prompt = "/"
	ti.Placeholder = "search"
	cmd := ti.Focus()
	ti.CharLimit = 200
	ti.SetWidth(max(1, m.layout.width-5))
	m.search.input = ti
	m.search.active = true
	m.search.historyIdx = len(m.search.history)
	m.layout.viewport.SetHeight(m.paneHeight() - 1)
	return cmd
}

// submitSearch searches the focused file or the tree's files, starting at the cursor.
func (m *Model) submitSearch() tea.Cmd {
	query := m.search.input.Value()
	m.cancelSearch()
	m.clearSearch()
	if strings.TrimSpace(query) == "" {
		return nil
	}

	m.search.term = strings.ToLower(query)
	m.appendSearchHistory(query)
	if m.currentContextLines() > 0 && m.file.name != "" {
		load := m.toggleCompactMode()
		seq, focus := m.nav.scanSeq, m.layout.focus
		return func() tea.Msg {
			return focusedSearchLoadedMsg{fileLoadedMsg: load().(fileLoadedMsg), searchSeq: seq, focus: focus}
		}
	}
	return m.searchFocusedContent()
}

func (m Model) handleFocusedSearchLoaded(msg focusedSearchLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.file.loadSeq || (msg.seq == m.file.canceledLoadSeq && msg.file == m.file.canceledLoadPath) {
		return m, nil
	}
	model, loadCmd := m.handleFileLoaded(msg.fileLoadedMsg)
	m = model.(Model)
	if msg.err != nil || msg.searchSeq != m.nav.scanSeq || msg.focus != m.layout.focus {
		return m, loadCmd
	}
	searchCmd := m.searchFocusedContent()
	m.layout.viewport.SetContent(m.renderDiff())
	return m, tea.Batch(loadCmd, searchCmd)
}

func (m *Model) searchFocusedContent() tea.Cmd {
	m.refreshSearchMatches()
	if m.layout.focus == paneTree && m.file.mdTOC == nil {
		return m.scanTree(treeScanSearch, true, true)
	}

	if len(m.search.matches) == 0 {
		return nil
	}

	idx := m.findFirstVisibleMatch(m.nav.diffCursor)
	if idx < 0 {
		return nil
	}
	m.search.cursor = idx
	m.nav.diffCursor = m.search.matches[idx]
	m.annot.cursorOnAnnotation = false
	m.syncTOCActiveSection()
	m.centerViewportOnCursor()
	return nil
}

// refreshSearchMatches retains the query when navigating to another file.
func (m *Model) refreshSearchMatches() {
	m.search.matches = nil
	m.search.matchSet = nil
	m.search.cursor = 0
	if m.search.term == "" {
		return
	}
	for i, dl := range m.file.lines {
		if dl.ChangeType != diff.ChangeDivider && strings.Contains(strings.ToLower(dl.Content), m.search.term) {
			m.search.matches = append(m.search.matches, i)
		}
	}
}

// nextSearchMatch advances to the next search match with wrap-around.
// in collapsed mode, hidden removed lines are skipped.
func (m *Model) nextSearchMatch() {
	if len(m.search.matches) == 0 {
		return
	}
	hunks := m.findHunks()
	start := m.search.cursor
	for {
		m.search.cursor = (m.search.cursor + 1) % len(m.search.matches)
		if !m.isCollapsedHidden(m.search.matches[m.search.cursor], hunks) {
			break
		}
		if m.search.cursor == start {
			return // all matches are hidden
		}
	}
	m.nav.diffCursor = m.search.matches[m.search.cursor]
	m.annot.cursorOnAnnotation = false
	m.centerViewportOnCursor()
}

// prevSearchMatch moves to the previous search match with wrap-around.
// in collapsed mode, hidden removed lines are skipped.
func (m *Model) prevSearchMatch() {
	if len(m.search.matches) == 0 {
		return
	}
	hunks := m.findHunks()
	start := m.search.cursor
	for {
		m.search.cursor--
		if m.search.cursor < 0 {
			m.search.cursor = len(m.search.matches) - 1
		}
		if !m.isCollapsedHidden(m.search.matches[m.search.cursor], hunks) {
			break
		}
		if m.search.cursor == start {
			return // all matches are hidden
		}
	}
	m.nav.diffCursor = m.search.matches[m.search.cursor]
	m.annot.cursorOnAnnotation = false
	m.centerViewportOnCursor()
}

// cancelSearch exits searching mode without submitting.
func (m *Model) cancelSearch() {
	m.search.active = false
	m.search.input.Blur()
	m.layout.viewport.SetHeight(m.paneHeight() - 1)
}

// appendSearchHistory records query in the in-session history. consecutive
// duplicates are skipped (less-style dedup). when the cap is exceeded, the
// oldest entries are dropped. historyIdx is reset to the draft slot so the
// next recall starts from "no recall active".
func (m *Model) appendSearchHistory(query string) {
	if n := len(m.search.history); n == 0 || m.search.history[n-1] != query {
		m.search.history = append(m.search.history, query)
		if len(m.search.history) > searchHistoryMax {
			m.search.history = m.search.history[len(m.search.history)-searchHistoryMax:]
		}
	}
	m.search.historyIdx = len(m.search.history)
}

// recallHistory walks the in-session search history. direction -1 walks toward
// older queries, +1 walks toward newer. clamps to [0, len(history)]; the
// upper bound represents the "draft" slot (input cleared, no recall active).
// no-op when history is empty.
func (m *Model) recallHistory(direction int) {
	if len(m.search.history) == 0 {
		return
	}
	idx := max(m.search.historyIdx+direction, 0)
	idx = min(idx, len(m.search.history))
	if idx == len(m.search.history) {
		m.search.input.SetValue("")
	} else {
		m.search.input.SetValue(m.search.history[idx])
	}
	m.search.input.CursorEnd()
	m.search.historyIdx = idx
}

// realignSearchCursor updates searchCursor to the nearest visible match at or after diffCursor.
// called after adjustCursorIfHidden moves diffCursor so the [X/Y] display stays accurate
// and n/N navigation starts from the correct position.
func (m *Model) realignSearchCursor() {
	if len(m.search.matches) == 0 {
		return
	}
	if idx := m.findFirstVisibleMatch(m.nav.diffCursor); idx >= 0 {
		m.search.cursor = idx
	}
}

// findFirstVisibleMatch returns the searchMatches index of the first visible match
// at or after startIdx, with wrap-around. returns -1 if no visible match exists.
func (m *Model) findFirstVisibleMatch(startIdx int) int {
	hunks := m.findHunks()
	// scan forward from startIdx
	for i, idx := range m.search.matches {
		if idx >= startIdx && !m.isCollapsedHidden(idx, hunks) {
			return i
		}
	}
	// wrap: scan from beginning
	for i, idx := range m.search.matches {
		if !m.isCollapsedHidden(idx, hunks) {
			return i
		}
	}
	return -1
}

// clearSearch resets per-query search state (term, matches, cursor, matchSet).
// session-scoped history fields are intentionally preserved.
func (m *Model) clearSearch() {
	m.nav.scanSeq++
	m.nav.scanKind = treeScanIdle
	m.search.term = ""
	m.search.matches = nil
	m.search.cursor = 0
	m.search.matchSet = nil
}

// buildSearchMatchSet converts searchMatches slice into a map for O(1) lookup during rendering.
// returns nil when there are no matches. callers assign the result to m.search.matchSet,
// which is read by render sub-methods (renderDiffLine, renderCollapsedAddLine, renderDeletePlaceholder)
// on the same value-receiver copy — the field acts as a render-pass local shared via the copy.
func (m Model) buildSearchMatchSet() map[int]bool {
	if len(m.search.matches) == 0 {
		return nil
	}
	result := make(map[int]bool, len(m.search.matches))
	for _, idx := range m.search.matches {
		result[idx] = true
	}
	return result
}

// handleSearchKey handles key messages during search input mode.
func (m Model) handleSearchKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		cmd := m.submitSearch()
		m.layout.viewport.SetContent(m.renderDiff()) // refresh viewport to clear/update highlights
		return m, cmd
	case "esc", "ctrl+c":
		m.cancelSearch()
		return m, nil
	case "up", "ctrl+p":
		m.recallHistory(-1)
		return m, nil
	case "down", "ctrl+n":
		m.recallHistory(+1)
		return m, nil
	default:
		var cmd tea.Cmd
		m.search.input, cmd = m.search.input.Update(msg)
		return m, cmd
	}
}
