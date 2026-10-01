package diff

import (
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
)

// StageHunk stages the contiguous change under the cursor, never the working
// file itself. Only ordinary tracked text modifications are supported. The
// captured patch is compared to every displayed change before touching the index.
func (g *Git) StageHunk(path string, displayed []DiffLine, cursor int) error {
	if cursor < 0 || cursor >= len(displayed) ||
		(displayed[cursor].ChangeType != ChangeAdd && displayed[cursor].ChangeType != ChangeRemove) {
		return errors.New("place the cursor on an added or removed line")
	}
	raw, err := g.runGit("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--no-renames",
		"--src-prefix=a/", "--dst-prefix=b/", "--unified=0", "--inter-hunk-context=0", "--", path)
	if err != nil {
		return err
	}
	start := strings.Index(raw, "\n@@ ")
	if start < 0 {
		return errors.New("no unstaged text hunk; reload the diff")
	}
	header := raw[:start+1]
	for _, unsupported := range []string{"new file mode ", "deleted file mode ", "old mode ", "new mode ", " 120000", " 160000"} {
		if strings.Contains(header, unsupported) {
			return errors.New("hunk staging supports regular tracked text modifications only")
		}
	}
	parsed, err := parseUnifiedDiff(raw, 0)
	if err != nil {
		return err
	}
	changes := func(lines []DiffLine) []DiffLine {
		var result []DiffLine
		for _, line := range lines {
			if line.ChangeType == ChangeAdd || line.ChangeType == ChangeRemove {
				result = append(result, line)
			}
		}
		return result
	}
	if !slices.Equal(changes(parsed), changes(displayed)) {
		return errors.New("file changed since display; reload before staging")
	}
	// Keep Git's exact hunk bytes, including no-final-newline markers and
	// quoted paths. A zero-context hunk matches revdiff's contiguous changes.
	hunks := strings.Split(raw[start+1:], "\n@@ ")
	for i, hunk := range hunks {
		if i > 0 {
			hunk = "@@ " + hunk
		}
		if !strings.HasSuffix(hunk, "\n") {
			hunk += "\n"
		}
		lines, parseErr := parseUnifiedDiff(header+hunk, 0)
		if parseErr != nil {
			return parseErr
		}
		if !slices.Contains(lines, displayed[cursor]) {
			continue
		}
		cmd := exec.Command("git", "apply", "--cached", "--unidiff-zero", "--whitespace=nowarn", "-")
		cmd.Dir, cmd.Env, cmd.Stdin = g.workDir, GitEnv(), strings.NewReader(header+hunk)
		if out, applyErr := cmd.CombinedOutput(); applyErr != nil {
			return fmt.Errorf("stage hunk: %w: %s", applyErr, strings.TrimSpace(string(out)))
		}
		return nil
	}
	return errors.New("hunk not found; reload the diff")
}
