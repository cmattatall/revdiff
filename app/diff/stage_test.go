package diff

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGitStageHunk(t *testing.T) {
	for _, name := range []string{"notes.txt", ":(glob)*.txt", "space and \"quote\".txt"} {
		t.Run(name, func(t *testing.T) {
			root := setupTestRepo(t)
			old := "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten"
			updated := "one\ninsert A\ninsert B\ntwo\nthree\nfour\nfive\nsix\nseven\nEIGHT\nnine\nten"
			path := filepath.Join(root, name)
			require.NoError(t, os.WriteFile(path, []byte(old), 0o600))
			gitCmd(t, root, "--literal-pathspecs", "add", "--", name)
			gitCmd(t, root, "commit", "-m", "baseline")
			require.NoError(t, os.WriteFile(path, []byte(updated), 0o600))
			g := NewGit(root)
			lines, err := g.FileDiff(FileDiffRequest{Path: name})
			require.NoError(t, err)
			cursor := slices.IndexFunc(lines, func(l DiffLine) bool { return l.Content == "EIGHT" })
			require.NoError(t, g.StageHunk(name, lines, cursor))
			indexed, err := g.runGit("show", ":"+name)
			require.NoError(t, err)
			require.Equal(t, strings.Replace(old, "eight", "EIGHT", 1), indexed)
			working, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, updated, string(working))
			// Reusing the displayed snapshot must not stage another change.
			require.Error(t, g.StageHunk(name, lines, cursor))
			lines, err = g.FileDiff(FileDiffRequest{Path: name})
			require.NoError(t, err)
			cursor = slices.IndexFunc(lines, func(l DiffLine) bool { return l.Content == "insert A" })
			require.NoError(t, g.StageHunk(name, lines, cursor))
			indexed, err = g.runGit("show", ":"+name)
			require.NoError(t, err)
			require.Equal(t, updated, indexed)
		})
	}
}

func TestGitStageHunkRejectsUnseenEdit(t *testing.T) {
	root := setupTestRepo(t)
	path := filepath.Join(root, "file.txt")
	require.NoError(t, os.WriteFile(path, []byte("before\n"), 0o600))
	gitCmd(t, root, "add", "file.txt")
	gitCmd(t, root, "commit", "-m", "baseline")
	require.NoError(t, os.WriteFile(path, []byte("reviewed\n"), 0o600))
	g := NewGit(root)
	lines, err := g.FileDiff(FileDiffRequest{Path: "file.txt"})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte("not reviewed\n"), 0o600))
	require.ErrorContains(t, g.StageHunk("file.txt", lines, 1), "file changed since display")
	indexed, err := g.runGit("show", ":file.txt")
	require.NoError(t, err)
	require.Equal(t, "before\n", indexed)
}
