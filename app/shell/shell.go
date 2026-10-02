// Package shell prepares interactive command sessions for the review terminal.
package shell

import (
	"os"
	"os/exec"
)

// Runner runs commands in the directory where revdiff was launched.
type Runner struct{}

// Prepare runs the command through $SHELL and waits for Enter before returning.
// The command is passed as an argument so the wrapper never interprets its text.
// Each run gets a clean alternate screen without changing the shell's scrollback.
func (Runner) Prepare(command string) *exec.Cmd {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	const script = `trap 'printf "\033[?1049l"' 0
trap 'exit 130' INT
trap 'exit 143' TERM
printf '\033[?1049h\033[2J\033[H'
"$1" -c "$2"
status=$?
printf '\nPress Enter to return to revdiff...'
IFS= read -r reply
exit "$status"`
	//nolint:gosec // the user explicitly requests this command through :!
	return exec.Command("/bin/sh", "-c", script, "revdiff", shell, command)
}
