package diff

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

// LineBlameRequest identifies a source line on either side of a diff.
type LineBlameRequest struct {
	FileDiffRequest
	Line    int
	Removed bool
}

// BlameDetails contains local attribution and optional GitHub links.
type BlameDetails struct {
	BlameLine
	CommitURL    string
	PullRequests []string
	PRStatus     string
}

// LineBlame loads just the requested line and looks for associated merged PRs.
func (g *Git) LineBlame(req LineBlameRequest) (BlameDetails, error) {
	if req.Line < 1 {
		return BlameDetails{}, fmt.Errorf("invalid blame line %d", req.Line)
	}
	file, ref, index := req.Path, g.blameTargetRef(req.Ref), req.Staged
	if req.Removed {
		if req.OldPath != "" {
			file = req.OldPath
		}
		ref, index = req.Ref, false
		switch {
		case strings.Contains(ref, "..."):
			left, right, _ := strings.Cut(ref, "...")
			base, err := g.runGit("merge-base", left, right)
			if err != nil {
				return BlameDetails{}, err
			}
			ref = strings.TrimSpace(base)
		case strings.Contains(ref, ".."):
			ref, _, _ = strings.Cut(ref, "..")
		case ref == "" && req.Staged:
			ref = "HEAD"
		case ref == "":
			index = true
		}
	}
	args := []string{"blame", "--line-porcelain", "-L", fmt.Sprintf("%d,%d", req.Line, req.Line)}
	if index {
		path, err := g.writeStagedBlameFile(file)
		if err != nil {
			return BlameDetails{}, err
		}
		defer func() { _ = os.Remove(path) }()
		args = append(args, "--contents", path)
	} else if ref != "" {
		args = append(args, ref)
	}
	args = append(args, "--", file)
	out, err := g.runGit(args...)
	if err != nil {
		return BlameDetails{}, err
	}
	lines, err := g.parseBlame(out)
	if err != nil {
		return BlameDetails{}, err
	}
	line, ok := lines[req.Line]
	if !ok {
		return BlameDetails{}, fmt.Errorf("no blame for %s:%d", file, req.Line)
	}
	details := BlameDetails{BlameLine: line}
	if strings.Trim(line.Commit, "0") == "" {
		details.PRStatus = "Uncommitted line"
		return details, nil
	}
	g.addBlameLinks(&details)
	return details, nil
}

// githubRepository accepts common origin URL forms without contacting a remote.
func (g *Git) githubRepository(remote string) string {
	remote = strings.TrimSpace(remote)
	var path string
	if rest, ok := strings.CutPrefix(remote, "git@github.com:"); ok {
		path = rest
	} else if u, err := url.Parse(remote); err == nil && u.Hostname() == "github.com" {
		path = strings.TrimPrefix(u.Path, "/")
	}
	path = strings.TrimSuffix(strings.TrimSuffix(path, "/"), ".git")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1])
}

func (g *Git) addBlameLinks(details *BlameDetails) {
	remote, err := g.runGit("remote", "get-url", "origin")
	if err != nil {
		details.PRStatus = "No origin remote"
		return
	}
	repo := g.githubRepository(remote)
	if repo == "" {
		details.PRStatus = "Origin is not hosted on github.com"
		return
	}
	base := "https://github.com/" + repo
	details.CommitURL = base + "/commit/" + details.Commit
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", "api", "--hostname", "github.com", "repos/"+repo+"/commits/"+details.Commit+"/pulls")
	cmd.Dir = g.workDir
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1")
	out, err := cmd.Output()
	if err != nil {
		details.PRStatus = "PR lookup unavailable (requires authenticated gh and network access)"
		return
	}
	var prs []struct {
		URL      string `json:"html_url"`
		MergedAt string `json:"merged_at"`
	}
	if err := json.Unmarshal(out, &prs); err != nil {
		details.PRStatus = "GitHub returned an unreadable PR response"
		return
	}
	for _, pr := range prs {
		if pr.MergedAt != "" && strings.HasPrefix(pr.URL, base+"/pull/") {
			details.PullRequests = append(details.PullRequests, pr.URL)
		}
	}
	if len(details.PullRequests) == 0 {
		details.PRStatus = "No associated merged PR found"
	}
}
