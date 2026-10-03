package ui

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/umputun/revdiff/app/diff"
	"github.com/umputun/revdiff/app/ui/overlay"
)

// InspectionOperation names read-only language queries, never editor actions.
type InspectionOperation string

const (
	InspectHover      InspectionOperation = "hover"
	InspectDefinition InspectionOperation = "definition"
	InspectReferences InspectionOperation = "references"
	InspectSymbols    InspectionOperation = "documentSymbol"
)

// InspectionPosition uses one-based source lines and zero-based UTF-8 byte columns.
type InspectionPosition struct {
	Path   string
	Line   int
	Column int
}

type InspectionResult struct {
	Text      string
	Markdown  bool
	Locations []InspectionPosition
	Symbols   []InspectionDocumentSymbol
}

type InspectionDocumentSymbol struct {
	Name     string
	Position InspectionPosition
}

type InspectionServer struct {
	Name    string
	Command string
	Path    string
}

type InspectionSymbol struct {
	Name   string
	Column int
}

type InspectionProgress struct {
	Server  string
	Message string
	Since   time.Time
}

// CodeInspector owns language servers and filesystem access outside the UI.
type CodeInspector interface {
	Warm(string) error
	Progress() []InspectionProgress
	Symbols(context.Context, InspectionPosition, string) ([]InspectionSymbol, error)
	Query(context.Context, InspectionOperation, InspectionPosition, string) (InspectionResult, error)
	ReadSource(context.Context, string) (string, error)
	Servers() []InspectionServer
}

type inspectionPageKind int

const (
	inspectionSymbols inspectionPageKind = iota
	inspectionLocations
	inspectionText
)

type inspectionPage struct {
	kind       inspectionPageKind
	spec       overlay.InspectionSpec
	targets    []InspectionPosition
	sourceLine string
}

type inspectionState struct {
	provider        CodeInspector
	installCommands map[string]string
	op              InspectionOperation
	page            inspectionPage
	history         []inspectionPage
	seq             uint64
	cancel          context.CancelFunc
	loadingSince    time.Time
	progress        string
}

type inspectionTickMsg time.Time

type inspectionLoadedMsg struct {
	seq  uint64
	page inspectionPage
	err  error
}

func (m Model) listLanguageServers() (tea.Model, tea.Cmd) {
	if m.inspection.provider == nil {
		m.keys.hint = "Code inspection requires a working-tree or all-files review"
		return m, nil
	}
	m.cancelInspection()
	m.inspection.history = nil
	m.showInspection(inspectionPage{kind: inspectionText, spec: overlay.InspectionSpec{Title: "Language servers", Text: "Loading…"}})
	seq, provider := m.inspection.seq, m.inspection.provider
	return m, func() tea.Msg {
		var rows []string
		for _, server := range provider.Servers() {
			status := "not on PATH"
			if server.Path != "" {
				status = server.Path
			}
			rows = append(rows, fmt.Sprintf("%s — %s (%s)\n  :lsp install %s", server.Name, server.Command, status, server.Name))
		}
		rows = append(rows, "Servers warm up when their first working-tree file loads. PATH discovery does not check server health.")
		return inspectionLoadedMsg{seq: seq, page: inspectionPage{kind: inspectionText,
			spec: overlay.InspectionSpec{Title: "Language servers", Text: strings.Join(rows, "\n\n")}}}
	}
}

func (m Model) openInspection(op InspectionOperation) (tea.Model, tea.Cmd) {
	if m.inspection.provider == nil {
		m.keys.hint = "Code inspection requires a working-tree or all-files review"
		return m, nil
	}
	if op != InspectSymbols && m.layout.focus != paneDiff {
		m.keys.hint = "Focus the diff and select a source line to inspect"
		return m, nil
	}
	if m.file.staged || m.cfg.staged {
		m.keys.hint = "Code inspection is unavailable for staged files"
		return m, nil
	}
	if !m.filesLoaded || m.file.requestedPath != "" {
		m.keys.hint = "Wait for the selected file to load"
		return m, nil
	}
	if op == InspectSymbols {
		return m.listFileSymbols()
	}
	line, ok := m.cursorDiffLine()
	if !ok || line.NewNum < 1 || line.ChangeType == diff.ChangeDivider || line.ChangeType == diff.ChangeRemove || line.IsBinary || m.annot.cursorOnAnnotation {
		m.keys.hint = "Select a current source line to inspect"
		return m, nil
	}
	m.cancelInspection()
	m.inspection.op, m.inspection.history = op, nil
	title := "Inspect " + string(op) + " · choose symbol"
	m.inspection.loadingSince = time.Now()
	m.showInspection(inspectionPage{kind: inspectionText, spec: overlay.InspectionSpec{Title: title, Text: "Loading…"}})
	ctx, cancel := context.WithCancel(context.Background())
	m.inspection.cancel = cancel
	seq, provider := m.inspection.seq, m.inspection.provider
	pos := InspectionPosition{Path: m.file.name, Line: line.NewNum}
	return m, func() tea.Msg {
		defer cancel()
		symbols, err := provider.Symbols(ctx, pos, line.Content)
		page := inspectionPage{kind: inspectionSymbols, sourceLine: line.Content,
			spec: overlay.InspectionSpec{Title: title, Items: []string{}}}
		for _, symbol := range symbols {
			position := pos
			position.Column = symbol.Column
			page.targets = append(page.targets, position)
			page.spec.Items = append(page.spec.Items, fmt.Sprintf("%s · column %d", symbol.Name, utf8.RuneCountInString(line.Content[:symbol.Column])+1))
		}
		if len(symbols) == 0 {
			page.kind, page.spec.Items, page.spec.Text = inspectionText, nil, "No inspectable symbols on this source line"
		}
		return inspectionLoadedMsg{seq: seq, page: page, err: err}
	}
}

func (m Model) listFileSymbols() (tea.Model, tea.Cmd) {
	m.cancelInspection()
	m.inspection.op, m.inspection.history = InspectSymbols, nil
	ctx, cancel := context.WithCancel(context.Background())
	m.inspection.cancel = cancel
	m.inspection.loadingSince = time.Now()
	seq, provider, path := m.inspection.seq, m.inspection.provider, m.file.name
	title := "Symbols · " + path
	m.showInspection(inspectionPage{kind: inspectionText, spec: overlay.InspectionSpec{Title: title, Text: "Loading…"}})
	return m, func() tea.Msg {
		defer cancel()
		response, err := provider.Query(ctx, InspectSymbols, InspectionPosition{Path: path}, "")
		page := inspectionPage{kind: inspectionLocations, spec: overlay.InspectionSpec{Title: title, Items: []string{}}}
		for _, symbol := range response.Symbols {
			page.targets = append(page.targets, symbol.Position)
			page.spec.Items = append(page.spec.Items, fmt.Sprintf("%s · line %d", symbol.Name, symbol.Position.Line))
		}
		if len(response.Symbols) == 0 {
			page.kind, page.spec.Items, page.spec.Text = inspectionText, nil, "No symbols reported for this file"
		}
		return inspectionLoadedMsg{seq: seq, page: page, err: err}
	}
}

func (m *Model) showInspection(page inspectionPage) {
	m.inspection.page = page
	m.overlay.OpenInspection(page.spec)
}

func (m *Model) cancelInspection() {
	if m.inspection.cancel != nil {
		m.inspection.cancel()
		m.inspection.cancel = nil
	}
	m.inspection.loadingSince = time.Time{}
	m.inspection.seq++
}

func (m Model) inspectionBack() (tea.Model, tea.Cmd) {
	m.cancelInspection()
	if n := len(m.inspection.history); n > 0 {
		page := m.inspection.history[n-1]
		m.inspection.history = m.inspection.history[:n-1]
		m.showInspection(page)
	} else {
		m.overlay.Close()
		m.inspection.page = inspectionPage{}
	}
	return m, nil
}

func (m Model) chooseInspection(index int) (tea.Model, tea.Cmd) {
	page := m.inspection.page
	if index < 0 || index >= len(page.targets) {
		return m, nil
	}
	position := page.targets[index]
	m.cancelInspection()
	ctx, cancel := context.WithCancel(context.Background())
	m.inspection.cancel = cancel
	m.inspection.loadingSince = time.Now()
	seq, provider, op := m.inspection.seq, m.inspection.provider, m.inspection.op
	m.inspection.history = append(m.inspection.history, page)
	title := fmt.Sprintf("%s · %s:%d", op, position.Path, position.Line)
	m.showInspection(inspectionPage{kind: inspectionText, spec: overlay.InspectionSpec{Title: title, Text: "Loading…"}})
	return m, func() tea.Msg {
		defer cancel()
		result := inspectionPage{kind: inspectionText, spec: overlay.InspectionSpec{Title: title}}
		if page.kind == inspectionLocations {
			source, err := provider.ReadSource(ctx, position.Path)
			result.spec.Text, result.spec.Line = source, position.Line
			result.spec.Highlighted = m.inspectionCode(position.Path, source)
			return inspectionLoadedMsg{seq: seq, page: result, err: err}
		}
		response, err := provider.Query(ctx, op, position, page.sourceLine)
		if err != nil {
			return inspectionLoadedMsg{seq: seq, err: err}
		}
		result.spec.Text = response.Text
		if response.Markdown {
			result.spec.Highlighted = m.inspectionMarkdown(position.Path, response.Text)
		}
		if op != InspectHover && len(response.Locations) > 0 {
			result.kind, result.targets = inspectionLocations, response.Locations
			result.spec.Items = make([]string, 0, len(response.Locations))
			for _, target := range response.Locations {
				result.spec.Items = append(result.spec.Items, fmt.Sprintf("%s:%d:%d", target.Path, target.Line, target.Column+1))
			}
		} else if result.spec.Text == "" {
			result.spec.Text = "No " + string(op) + " information for this symbol"
		}
		return inspectionLoadedMsg{seq: seq, page: result}
	}
}

func (m Model) handleInspectionLoaded(msg inspectionLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.inspection.seq || m.overlay.Kind() != overlay.KindInspection {
		return m, nil
	}
	m.inspection.cancel = nil
	m.inspection.loadingSince = time.Time{}
	if msg.err != nil {
		m.showInspection(inspectionPage{kind: inspectionText, spec: overlay.InspectionSpec{Title: "Code inspection", Text: msg.err.Error()}})
	} else {
		m.showInspection(msg.page)
	}
	return m, nil
}

func (m Model) warmInspection() tea.Cmd {
	if m.inspection.provider == nil || m.file.staged || m.cfg.staged || m.file.name == "" {
		return nil
	}
	provider, path := m.inspection.provider, m.file.name
	return func() tea.Msg {
		// Unsupported files and missing servers are only errors on explicit inspection.
		_ = provider.Warm(path)
		return nil
	}
}

func (m Model) inspectionTick() tea.Cmd {
	if m.inspection.provider == nil {
		return nil
	}
	return tea.Tick(time.Second, func(now time.Time) tea.Msg { return inspectionTickMsg(now) })
}

func (m Model) handleInspectionTick(now time.Time) (tea.Model, tea.Cmd) {
	if m.inspection.provider == nil {
		return m, nil
	}
	var progress []string
	for _, status := range m.inspection.provider.Progress() {
		text := strings.Join(strings.Fields(diff.SanitizeCommitText(status.Server+": "+status.Message)), " ")
		progress = append(progress, fmt.Sprintf("%s · %s", text, max(0, now.Sub(status.Since)).Truncate(time.Second)))
	}
	m.inspection.progress = strings.Join(progress, " | ")
	if !m.inspection.loadingSince.IsZero() && m.overlay.Kind() == overlay.KindInspection {
		page := m.inspection.page
		page.spec.Text = fmt.Sprintf("Loading… %s", max(0, now.Sub(m.inspection.loadingSince)).Truncate(time.Second))
		if m.inspection.progress != "" {
			page.spec.Text += "\n\n" + m.inspection.progress
		}
		page.spec.Text += "\n\nEsc returns to the review. Background indexing continues."
		m.showInspection(page)
	}
	return m, m.inspectionTick()
}

// inspectionCode sanitizes server/file content before adding our own ANSI colors.
// It runs in the query command, not in the render or keyboard event loop.
func (m Model) inspectionCode(language, source string) string {
	source = strings.ReplaceAll(diff.SanitizeCommitText(source), "\t", "    ")
	lines := strings.Split(source, "\n")
	diffLines := make([]diff.DiffLine, len(lines))
	for n, line := range lines {
		diffLines[n] = diff.DiffLine{Content: line, ChangeType: diff.ChangeContext}
	}
	if m.highlighter != nil {
		if highlighted := m.highlighter.HighlightLines(language, diffLines); highlighted != nil {
			return strings.Join(highlighted, "\n")
		}
	}
	return source
}

var inspectionFence = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})(.*)$")

// inspectionMarkdown replaces fenced blocks with highlighted source while
// keeping the surrounding documentation. Unlabelled blocks use the source file's language.
func (m Model) inspectionMarkdown(path, text string) string {
	lines := strings.Split(strings.ReplaceAll(diff.SanitizeCommitText(text), "\t", "    "), "\n")
	var rendered []string
	for n := 0; n < len(lines); n++ {
		fence := inspectionFence.FindStringSubmatch(lines[n])
		if fence == nil {
			rendered = append(rendered, lines[n])
			continue
		}
		language := path
		if info := strings.Fields(fence[2]); len(info) > 0 {
			language = info[0]
		}
		end := n + 1
		for ; end < len(lines); end++ {
			closeFence := inspectionFence.FindStringSubmatch(lines[end])
			if closeFence != nil && closeFence[1][0] == fence[1][0] && len(closeFence[1]) >= len(fence[1]) && strings.TrimSpace(closeFence[2]) == "" {
				break
			}
		}
		rendered = append(rendered, m.inspectionCode(language, strings.Join(lines[n+1:end], "\n")))
		n = end
	}
	return strings.Join(rendered, "\n")
}
