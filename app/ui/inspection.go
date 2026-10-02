package ui

import (
	"context"
	"fmt"
	"regexp"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/umputun/revdiff/app/diff"
	"github.com/umputun/revdiff/app/ui/overlay"
)

// InspectionOperation names read-only language queries, never editor actions.
type InspectionOperation string

const (
	InspectHover      InspectionOperation = "hover"
	InspectDefinition InspectionOperation = "definition"
	InspectReferences InspectionOperation = "references"
)

// InspectionPosition uses one-based source lines and zero-based UTF-8 byte columns.
type InspectionPosition struct {
	Path   string
	Line   int
	Column int
}

type InspectionResult struct {
	Text      string
	Locations []InspectionPosition
}

// CodeInspector owns language servers and filesystem access outside the UI.
type CodeInspector interface {
	Query(context.Context, InspectionOperation, InspectionPosition, string) (InspectionResult, error)
	ReadSource(context.Context, string) (string, error)
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
}

type inspectionLoadedMsg struct {
	seq  uint64
	page inspectionPage
	err  error
}

// Servers decide meaning. The picker only finds identifier-shaped spans and
// preserves byte offsets, including repeated names and non-ASCII identifiers.
var inspectionIdentifier = regexp.MustCompile(`[\pL_$][\pL\pN\pM_$]*`)

func (m Model) openInspection(op InspectionOperation) (tea.Model, tea.Cmd) {
	if m.inspection.provider == nil {
		m.keys.hint = "Code inspection requires a working-tree or all-files review"
		return m, nil
	}
	if m.layout.focus != paneDiff {
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
	line, ok := m.cursorDiffLine()
	if !ok || line.NewNum < 1 || line.ChangeType == diff.ChangeDivider || line.ChangeType == diff.ChangeRemove || line.IsBinary || m.annot.cursorOnAnnotation {
		m.keys.hint = "Select a current source line to inspect"
		return m, nil
	}
	page := inspectionPage{kind: inspectionSymbols, sourceLine: line.Content,
		spec: overlay.InspectionSpec{Title: "Inspect " + string(op) + " · choose symbol", Items: []string{}}}
	for _, span := range inspectionIdentifier.FindAllStringIndex(line.Content, -1) {
		column := span[0]
		page.targets = append(page.targets, InspectionPosition{Path: m.file.name, Line: line.NewNum, Column: column})
		page.spec.Items = append(page.spec.Items, fmt.Sprintf("%s · column %d", line.Content[span[0]:span[1]], utf8.RuneCountInString(line.Content[:column])+1))
	}
	if len(page.targets) == 0 {
		m.keys.hint = "No identifiers on this source line"
		return m, nil
	}
	m.cancelInspection()
	m.inspection.op, m.inspection.history = op, nil
	m.showInspection(page)
	return m, nil
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	m.inspection.cancel = cancel
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
			return inspectionLoadedMsg{seq: seq, page: result, err: err}
		}
		response, err := provider.Query(ctx, op, position, page.sourceLine)
		if err != nil {
			return inspectionLoadedMsg{seq: seq, err: err}
		}
		result.spec.Text = response.Text
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
	if msg.err != nil {
		m.showInspection(inspectionPage{kind: inspectionText, spec: overlay.InspectionSpec{Title: "Code inspection", Text: msg.err.Error()}})
	} else {
		m.showInspection(msg.page)
	}
	return m, nil
}
