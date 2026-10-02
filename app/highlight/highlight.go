package highlight

import (
	"fmt"
	"log"
	"slices"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/dlclark/regexp2/v2"

	"github.com/umputun/revdiff/app/diff"
)

// maxBacktrackingStack raises regexp2's default 100k-slot cap for long single-line tokens. The
// 40k-character Go string that exposed the regression needs 240,045 slots, leaving about four
// times headroom. Each slot is an int, so one million slots allow about 8 MB for runtrack on
// 64-bit targets. regexp2 caps only runtrack; runstack grows through doubleIntSlice with no limit
// check, so the two stacks together can use roughly twice the nominal runtrack budget.
//
// Chroma compiles lexer rules with a bare regexp2.Compile and sets a separate 250ms MatchTimeout on
// each rule. matchRules ignores either error and tries later rules, so affected text can lose its
// intended color or fall back to chroma.Error. Raising this cap cannot help once the timeout is the
// binding ceiling. This finite budget does not restore regexp2's former unbounded behavior, and
// larger tokens can still exceed either ceiling.
const maxBacktrackingStack = 1_000_000

// chroma defers rule compilation until a lexer is first used, and a Highlighter is the only way
// into that path, so raising the package default here reaches every lexer revdiff builds.
var raiseBacktrackingCap = sync.OnceFunc(func() {
	regexp2.DefaultOptimizationOptions.MaxBacktrackingStackSize = maxBacktrackingStack
})

// chromaFallbackStyle is the name of the Chroma style that doubles as styles.Fallback.
// styles.Get returns Fallback for unknown names, but "swapoff" is a real built-in style
// whose registry entry IS the Fallback sentinel, so we must special-case it.
const chromaFallbackStyle = "swapoff"

// Highlighter applies syntax highlighting to source code lines using Chroma.
type Highlighter struct {
	mu        sync.Mutex
	styleName string
	enabled   bool
	cache     []highlightEntry // oldest first; immutable content, not file timestamps
	cacheSize int
}

type highlightEntry struct {
	filename, style string
	lines           []diff.DiffLine
	highlighted     []string
	size            int
}

const maxHighlightCacheBytes = 16 << 20

// New creates a Highlighter with the given Chroma style name and enabled state.
// If styleName is empty, defaults to "monokai". Logs a warning if the style name is unknown.
// It also changes regexp2.DefaultOptimizationOptions.MaxBacktrackingStackSize process-wide before
// Chroma compiles lexer rules.
func New(styleName string, enabled bool) *Highlighter {
	raiseBacktrackingCap()
	if styleName == "" {
		styleName = "monokai"
	}
	if styles.Get(styleName) == styles.Fallback && styleName != chromaFallbackStyle {
		log.Printf("[WARN] unknown chroma style %q, using monokai", styleName)
		styleName = "monokai"
	}
	return &Highlighter{styleName: styleName, enabled: enabled}
}

// SetStyle changes the Chroma style used for subsequent HighlightLines calls.
// Returns false if the style name is unknown.
func (h *Highlighter) SetStyle(styleName string) bool {
	if styles.Get(styleName) == styles.Fallback && styleName != chromaFallbackStyle {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.styleName = styleName
	return true
}

// StyleName returns the current Chroma style name.
func (h *Highlighter) StyleName() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.styleName
}

// IsValidStyle reports whether styleName is a known Chroma style.
func IsValidStyle(styleName string) bool {
	return styles.Get(styleName) != styles.Fallback || styleName == chromaFallbackStyle
}

// CachedLines returns an already-tokenized version without doing lexer work.
// UI callers can use it for an immediate colored render on repeat visits.
func (h *Highlighter) CachedLines(filename string, lines []diff.DiffLine) ([]string, bool) {
	if !h.enabled || len(lines) == 0 {
		return nil, true
	}
	return h.cached(filename, h.StyleName(), lines)
}

// HighlightLines takes a filename (for lexer detection) and a slice of diff.DiffLine,
// reconstructs the file content, tokenizes it with Chroma, and returns a parallel []string
// where each entry contains the ANSI-formatted (foreground-only) version of that line's content.
// returns nil if highlighting is disabled or no lexer matches the filename.
func (h *Highlighter) HighlightLines(filename string, lines []diff.DiffLine) []string {
	if !h.enabled || len(lines) == 0 {
		return nil
	}
	styleName := h.StyleName()
	if cached, ok := h.cached(filename, styleName, lines); ok {
		return cached
	}

	lexer := lexers.Match(filename)
	if lexer == nil {
		return nil
	}
	lexer = chroma.Coalesce(lexer)

	style := styles.Get(styleName)

	newContent, oldContent := h.reconstructFiles(lines)

	newHL := h.highlightFile(lexer, style, newContent)
	oldHL := h.highlightFile(lexer, style, oldContent)

	result := h.mapHighlightedLines(lines, newHL, oldHL)
	h.remember(filename, styleName, lines, result)
	return result
}

// Compare the complete diff, not just changed lines: context and staged versions
// can differ independently. Clone slice headers' backing storage at the boundary
// so callers cannot corrupt a cached result. Strings themselves are immutable.
func (h *Highlighter) cached(filename, style string, lines []diff.DiffLine) ([]string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, entry := range h.cache {
		if entry.filename == filename && entry.style == style && slices.Equal(entry.lines, lines) {
			copy(h.cache[i:], h.cache[i+1:])
			h.cache[len(h.cache)-1] = entry
			return slices.Clone(entry.highlighted), true
		}
	}
	return nil, false
}

func (h *Highlighter) remember(filename, style string, lines []diff.DiffLine, highlighted []string) {
	size := len(filename) + len(style) + len(lines)*80 + len(highlighted)*16
	for _, line := range lines {
		size += len(line.Content)
	}
	for _, line := range highlighted {
		size += len(line)
	}
	if size > maxHighlightCacheBytes {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for len(h.cache) > 0 && (len(h.cache) >= 32 || h.cacheSize+size > maxHighlightCacheBytes) {
		h.cacheSize -= h.cache[0].size
		h.cache = slices.Delete(h.cache, 0, 1)
	}
	h.cache = append(h.cache, highlightEntry{filename, style, slices.Clone(lines), slices.Clone(highlighted), size})
	h.cacheSize += size
}

// reconstructFiles builds old and new file content from diff lines.
// new file = context + added lines, old file = context + removed lines.
func (h *Highlighter) reconstructFiles(lines []diff.DiffLine) (newFile, oldFile string) {
	var newB, oldB strings.Builder
	for _, dl := range lines {
		switch dl.ChangeType {
		case diff.ChangeAdd:
			newB.WriteString(dl.Content)
			newB.WriteByte('\n')
		case diff.ChangeRemove:
			oldB.WriteString(dl.Content)
			oldB.WriteByte('\n')
		case diff.ChangeDivider:
			// skip dividers
		default: // context
			newB.WriteString(dl.Content)
			newB.WriteByte('\n')
			oldB.WriteString(dl.Content)
			oldB.WriteByte('\n')
		}
	}
	return newB.String(), oldB.String()
}

// highlightFile tokenizes source code and returns per-line ANSI strings with foreground-only colors.
func (h *Highlighter) highlightFile(lexer chroma.Lexer, style *chroma.Style, source string) []string {
	if source == "" {
		return nil
	}

	iter, err := lexer.Tokenise(nil, source)
	if err != nil {
		log.Printf("[WARN] syntax highlighting tokenization failed: %v", err)
		return nil
	}

	var lines []string
	var cur strings.Builder

	for _, tok := range iter.Tokens() {
		// tokens may contain embedded newlines, split them
		parts := strings.Split(tok.Value, "\n")
		for i, part := range parts {
			if i > 0 {
				// newline boundary: flush current line
				lines = append(lines, cur.String())
				cur.Reset()
			}
			if part == "" {
				continue
			}
			writeTokenANSI(&cur, tok.Type, part, style)
		}
	}
	// flush last line
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}

	return lines
}

// writeTokenANSI writes a token value with foreground-only ANSI escape codes.
// uses specific attribute resets instead of full reset (\033[0m) to preserve
// any outer background color set by lipgloss.
func writeTokenANSI(b *strings.Builder, tokenType chroma.TokenType, value string, style *chroma.Style) {
	entry := style.Get(tokenType)
	hasFg, hasBold, hasItalic := false, false, false

	if entry.Colour.IsSet() { //nolint:misspell // chroma API uses British spelling
		r, g, bb := entry.Colour.Red(), entry.Colour.Green(), entry.Colour.Blue() //nolint:misspell // chroma API
		fmt.Fprintf(b, "\033[38;2;%d;%d;%dm", r, g, bb)
		hasFg = true
	}
	if entry.Bold == chroma.Yes {
		b.WriteString("\033[1m")
		hasBold = true
	}
	if entry.Italic == chroma.Yes {
		b.WriteString("\033[3m")
		hasItalic = true
	}

	b.WriteString(value)

	// reset only the attributes we set, preserving outer background
	if hasFg {
		b.WriteString("\033[39m") // reset foreground to default
	}
	if hasBold {
		b.WriteString("\033[22m") // reset bold
	}
	if hasItalic {
		b.WriteString("\033[23m") // reset italic
	}
}

// mapHighlightedLines maps highlighted old/new file lines back to the original diff line order.
func (h *Highlighter) mapHighlightedLines(lines []diff.DiffLine, newHL, oldHL []string) []string {
	result := make([]string, len(lines))
	var newIdx, oldIdx int

	for i, dl := range lines {
		switch dl.ChangeType {
		case diff.ChangeAdd:
			if newIdx < len(newHL) {
				result[i] = newHL[newIdx]
			}
			newIdx++
		case diff.ChangeRemove:
			if oldIdx < len(oldHL) {
				result[i] = oldHL[oldIdx]
			}
			oldIdx++
		case diff.ChangeDivider:
			// no highlighted content for dividers
		default: // context
			if newIdx < len(newHL) {
				result[i] = newHL[newIdx]
			}
			newIdx++
			oldIdx++
		}
	}
	return result
}
