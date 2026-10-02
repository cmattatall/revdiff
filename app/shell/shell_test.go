package shell

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunnerPrepare(t *testing.T) {
	for _, shell := range []string{"", "/bin/bash", "/bin/zsh"} {
		t.Run(shell, func(t *testing.T) {
			if shell != "" {
				if _, err := exec.LookPath(shell); err != nil {
					t.Skip("shell not installed")
				}
			}
			t.Setenv("SHELL", shell)
			cmd := (Runner{}).Prepare(`printf '%s\n' 'Mixed Case; $HOME' | tr ' ' '_'`)
			cmd.Stdin = strings.NewReader("\n")
			out, err := cmd.CombinedOutput()
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(string(out), "\x1b[?1049h\x1b[2J\x1b[H"), "start with a fresh command screen")
			require.Contains(t, string(out), "Mixed_Case;_$HOME\n")
			require.Contains(t, string(out), "Press Enter to return to revdiff")
			require.True(t, strings.HasSuffix(string(out), "\x1b[?1049l"), "restore the previous screen")
		})
	}
}

func TestRunnerPreservesCommandFailure(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	cmd := (Runner{}).Prepare("exit 7")
	cmd.Stdin = strings.NewReader("\n")
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	require.True(t, errors.As(err, &exit))
	require.Equal(t, 7, exit.ExitCode())
	require.Contains(t, string(out), "Press Enter")
	require.True(t, strings.HasSuffix(string(out), "\x1b[?1049l"), "restore the previous screen after failure too")
}
