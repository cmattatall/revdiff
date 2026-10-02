package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/umputun/revdiff/app/diff"
	"github.com/umputun/revdiff/app/ui/overlay"
)

type lineBlamer interface {
	LineBlame(diff.LineBlameRequest) (diff.BlameDetails, error)
}

type blameDetailsMsg struct {
	seq     uint64
	target  string
	details diff.BlameDetails
	err     error
}

func (m Model) openBlameView() (tea.Model, tea.Cmd) {
	if m.layout.focus != paneDiff {
		m.keys.hint = "Focus the diff to inspect line blame"
		return m, nil
	}
	line, ok := m.cursorDiffLine()
	if !ok || line.ChangeType == diff.ChangeDivider || line.IsBinary {
		m.keys.hint = "Select a source line to inspect blame"
		return m, nil
	}
	provider, ok := m.blamer.(lineBlamer)
	if !ok {
		m.keys.hint = "Line blame details require a Git review"
		return m, nil
	}
	req := diff.LineBlameRequest{
		FileDiffRequest: diff.FileDiffRequest{
			Ref: m.cfg.ref, Path: m.file.name, OldPath: m.file.oldName, Staged: m.file.staged || m.cfg.staged,
		},
		Line: line.NewNum, Removed: line.ChangeType == diff.ChangeRemove,
	}
	path := req.Path
	if req.Removed {
		req.Line = line.OldNum
		if req.OldPath != "" {
			path = req.OldPath
		}
	}
	target := fmt.Sprintf("%s:%d", path, req.Line)
	m.blameViewSeq++
	seq := m.blameViewSeq
	m.overlay.OpenBlame(overlay.InfoSpec{
		HeaderText: "Blame: " + target,
		Rows:       []overlay.InfoRow{{Label: "Status", Value: "Loading blame and associated PRs…"}},
	})
	return m, func() tea.Msg {
		details, err := provider.LineBlame(req)
		return blameDetailsMsg{seq: seq, target: target, details: details, err: err}
	}
}

func (m Model) handleBlameDetails(msg blameDetailsMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.blameViewSeq || m.overlay.Kind() != overlay.KindBlame {
		return m, nil
	}
	spec := overlay.InfoSpec{HeaderText: "Blame: " + msg.target, FooterText: "Esc to close"}
	if msg.err != nil {
		spec.Rows = []overlay.InfoRow{{Label: "Error", Value: msg.err.Error()}}
	} else {
		d := msg.details
		commit := d.Commit
		if strings.Trim(commit, "0") == "" {
			commit = "Uncommitted"
		}
		spec.Rows = []overlay.InfoRow{
			{Label: "Commit", Value: commit},
			{Label: "Author", Value: d.Author},
			{Label: "Summary", Value: d.Summary},
			{Label: "Commit URL", Value: d.CommitURL},
		}
		if !d.Time.IsZero() {
			spec.Rows = append(spec.Rows, overlay.InfoRow{Label: "Date", Value: d.Time.Format("2006-01-02 15:04:05 MST")})
		}
		for _, link := range d.PullRequests {
			spec.Rows = append(spec.Rows, overlay.InfoRow{Label: "Merged PR", Value: link})
		}
		if d.PRStatus != "" {
			spec.Rows = append(spec.Rows, overlay.InfoRow{Label: "PR lookup", Value: d.PRStatus})
		}
	}
	m.overlay.UpdateBlame(spec)
	return m, nil
}
