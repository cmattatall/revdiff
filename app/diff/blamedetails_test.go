package diff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGitLineBlameSides(t *testing.T) {
	dir := setupTestRepo(t)
	g := NewGit(dir)
	writeFile(t, dir, "f.txt", "first\noriginal\n")
	gitCmd(t, dir, "add", "f.txt")
	gitCmd(t, dir, "commit", "-m", "Initial attribution")
	initial, err := g.runGit("rev-parse", "HEAD")
	require.NoError(t, err)
	writeFile(t, dir, "f.txt", "first\ncommitted\n")
	gitCmd(t, dir, "commit", "-am", "Second attribution")
	second, err := g.runGit("rev-parse", "HEAD")
	require.NoError(t, err)
	writeFile(t, dir, "f.txt", "first\nindex edit\n")
	gitCmd(t, dir, "add", "f.txt")
	writeFile(t, dir, "f.txt", "first\nworktree edit\n")
	for _, tc := range []struct {
		name, ref       string
		staged, removed bool
		commit, summary string
	}{
		{"worktree", "", false, false, strings.Repeat("0", 40), "Version of f.txt from f.txt"},
		{"index", "", true, false, strings.Repeat("0", 40), ""},
		{"removed index edit", "", false, true, strings.Repeat("0", 40), ""},
		{"removed from staged", "", true, true, strings.TrimSpace(second), "Second attribution"},
		{"historical new side", "HEAD~1..HEAD", false, false, strings.TrimSpace(second), "Second attribution"},
		{"historical old side", "HEAD~1..HEAD", false, true, strings.TrimSpace(initial), "Initial attribution"},
		{"single ref old side", "HEAD~1", false, true, strings.TrimSpace(initial), "Initial attribution"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			detail, err := g.LineBlame(LineBlameRequest{FileDiffRequest: FileDiffRequest{Path: "f.txt", Ref: tc.ref, Staged: tc.staged}, Line: 2, Removed: tc.removed})
			require.NoError(t, err)
			require.Equal(t, tc.commit, detail.Commit)
			if tc.summary != "" {
				require.Equal(t, tc.summary, detail.Summary)
			}
			require.Empty(t, detail.PullRequests)
		})
	}
}

func TestGitLineBlameRenamedOldSide(t *testing.T) {
	dir := setupTestRepo(t)
	g := NewGit(dir)
	writeFile(t, dir, "old.txt", "original\n")
	gitCmd(t, dir, "add", "old.txt")
	gitCmd(t, dir, "commit", "-m", "Before rename")
	gitCmd(t, dir, "mv", "old.txt", "new.txt")
	writeFile(t, dir, "new.txt", "replacement\n")
	gitCmd(t, dir, "commit", "-am", "Rename and replace")
	detail, err := g.LineBlame(LineBlameRequest{FileDiffRequest: FileDiffRequest{Path: "new.txt", OldPath: "old.txt", Ref: "HEAD~1..HEAD"}, Line: 1, Removed: true})
	require.NoError(t, err)
	require.Equal(t, "Before rename", detail.Summary)
}

func TestGitLineBlameTripleDotUsesMergeBase(t *testing.T) {
	dir := setupTestRepo(t)
	g := NewGit(dir)
	writeFile(t, dir, "f.txt", "common ancestor\n")
	gitCmd(t, dir, "add", "f.txt")
	gitCmd(t, dir, "commit", "-m", "Common ancestor")
	gitCmd(t, dir, "branch", "base")
	gitCmd(t, dir, "checkout", "-b", "left")
	writeFile(t, dir, "f.txt", "left branch\n")
	gitCmd(t, dir, "commit", "-am", "Left change")
	gitCmd(t, dir, "checkout", "-b", "right", "base")
	writeFile(t, dir, "f.txt", "right branch\n")
	gitCmd(t, dir, "commit", "-am", "Right change")
	detail, err := g.LineBlame(LineBlameRequest{FileDiffRequest: FileDiffRequest{Path: "f.txt", Ref: "left...right"}, Line: 1, Removed: true})
	require.NoError(t, err)
	require.Equal(t, "Common ancestor", detail.Summary)
}

func TestGitBlameGitHubLinks(t *testing.T) {
	dir := setupTestRepo(t)
	g := NewGit(dir)
	gitCmd(t, dir, "remote", "add", "origin", "git@github.com:example/project.git")
	bin := t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$GH_ARGS\"\nprintf '%s' \"$GH_RESPONSE\"\nexit \"${GH_EXIT:-0}\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o700))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GH_ARGS", filepath.Join(bin, "args"))
	t.Setenv("GH_RESPONSE", `[{"html_url":"https://github.com/example/project/pull/12","merged_at":"2026-01-01"},{"html_url":"https://github.com/example/project/pull/13","merged_at":null}]`)
	d := BlameDetails{BlameLine: BlameLine{Commit: strings.Repeat("a", 40)}}
	g.addBlameLinks(&d)
	require.Equal(t, []string{"https://github.com/example/project/pull/12"}, d.PullRequests)
	require.Equal(t, "https://github.com/example/project/commit/"+strings.Repeat("a", 40), d.CommitURL)
	args, err := os.ReadFile(filepath.Join(bin, "args"))
	require.NoError(t, err)
	require.Equal(t, "api\n--hostname\ngithub.com\nrepos/example/project/commits/"+strings.Repeat("a", 40)+"/pulls\n", string(args))
	t.Setenv("GH_EXIT", "1")
	d = BlameDetails{BlameLine: BlameLine{Commit: strings.Repeat("b", 40)}}
	g.addBlameLinks(&d)
	require.Contains(t, d.PRStatus, "unavailable")
	require.NotEmpty(t, d.CommitURL, "local commit URL survives a failed PR lookup")
}

func TestGitHubRepository(t *testing.T) {
	g := Git{}
	for _, remote := range []string{"git@github.com:example/project.git", "https://github.com/example/project.git", "ssh://git@github.com/example/project", "https://github.com/example/project/"} {
		require.Equal(t, "example/project", g.githubRepository(remote))
	}
	for _, remote := range []string{"/local/repo", "https://github.com.evil.invalid/example/project", "git@gitlab.com:example/project.git", "https://github.com/example"} {
		require.Empty(t, g.githubRepository(remote))
	}
}
