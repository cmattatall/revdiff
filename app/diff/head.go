package diff

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// RepositoryHead identifies the current checkout, independently of the review ref.
type RepositoryHead struct {
	Branch string // empty when HEAD is detached
	Commit string // abbreviated SHA, empty before the first commit
}

// RepositoryHead reads the checkout's branch and HEAD without scanning file changes.
func (g *Git) RepositoryHead() (RepositoryHead, error) {
	var values [2]string
	for i, args := range [][]string{
		{"symbolic-ref", "--quiet", "--short", "HEAD"},
		{"rev-parse", "--verify", "--quiet", "--short=7", "HEAD"},
	} {
		cmd := exec.CommandContext(context.Background(), "git", args...) //nolint:gosec // fixed Git arguments
		cmd.Dir, cmd.Env = g.workDir, GitEnv()
		out, err := cmd.Output()
		if err != nil {
			// Exit 1 means detached HEAD or an unborn branch, respectively.
			if exitErr, ok := errors.AsType[*exec.ExitError](err); ok && exitErr.ExitCode() == 1 {
				continue
			}
			return RepositoryHead{}, fmt.Errorf("read repository HEAD: %w", err)
		}
		values[i] = strings.TrimSpace(string(out))
	}
	return RepositoryHead{Branch: values[0], Commit: values[1]}, nil
}
