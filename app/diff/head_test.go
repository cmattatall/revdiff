package diff

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGit_RepositoryHead(t *testing.T) {
	dir := setupTestRepo(t)
	gitCmd(t, dir, "symbolic-ref", "HEAD", "refs/heads/feature/footer")
	g := NewGit(dir)
	head, err := g.RepositoryHead()
	require.NoError(t, err)
	require.Equal(t, RepositoryHead{Branch: "feature/footer"}, head)

	gitCmd(t, dir, "commit", "--allow-empty", "-m", "first")
	full, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output() //nolint:gosec // temporary test repository
	require.NoError(t, err)
	sha := strings.TrimSpace(string(full))[:7]
	head, err = g.RepositoryHead()
	require.NoError(t, err)
	require.Equal(t, RepositoryHead{Branch: "feature/footer", Commit: sha}, head)

	linked := filepath.Join(t.TempDir(), "linked")
	gitCmd(t, dir, "worktree", "add", "-b", "linked-branch", linked, "HEAD")
	head, err = NewGit(linked).RepositoryHead()
	require.NoError(t, err)
	require.Equal(t, RepositoryHead{Branch: "linked-branch", Commit: sha}, head)

	gitCmd(t, dir, "checkout", "--detach", "HEAD")
	head, err = g.RepositoryHead()
	require.NoError(t, err)
	require.Equal(t, RepositoryHead{Commit: sha}, head)

	_, err = NewGit(t.TempDir()).RepositoryHead()
	require.Error(t, err)
}
