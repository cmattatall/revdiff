package diff

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// StageFile stages the current working-tree file, including deletion or rename.
// Unlike StageHunk, this deliberately includes changes made since the last render.
func (g *Git) StageFile(path, oldPath string) error {
	paths := []string{path}
	if oldPath != "" && oldPath != path {
		paths = append(paths, oldPath)
	}
	for _, p := range paths {
		if !filepath.IsLocal(p) || filepath.Clean(p) == "." {
			return errors.New("select a file inside the repository")
		}
		info, err := os.Lstat(filepath.Join(g.workDir, p))
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("inspect file for staging: %w", err)
		}
		if err == nil && !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			return errors.New("select a file, not a directory or submodule")
		}
	}
	_, err := g.runGit(append([]string{"add", "-A", "--"}, paths...)...)
	return err
}

// UnstageFile restores the selected index paths to HEAD without changing the
// working tree. Git also supports an unborn HEAD here, removing initial additions.
func (g *Git) UnstageFile(path, oldPath string) error {
	paths := []string{path}
	if oldPath != "" && oldPath != path {
		paths = append(paths, oldPath)
	}
	for _, p := range paths {
		if !filepath.IsLocal(p) || filepath.Clean(p) == "." {
			return errors.New("select a file inside the repository")
		}
	}
	// Validate against the index, not the working copy: it may already have
	// been deleted, renamed, or replaced by a directory since it was staged.
	changed, err := g.runGit(append([]string{"diff", "--cached", "--name-only", "--no-renames", "-z", "--"}, paths...)...)
	if err != nil {
		return err
	}
	for _, p := range strings.Split(strings.TrimSuffix(changed, "\x00"), "\x00") {
		if p != "" && !slices.Contains(paths, p) {
			return errors.New("select a file, not a directory")
		}
	}
	_, err = g.runGit(append([]string{"reset", "--quiet", "--"}, paths...)...)
	return err
}

// StageHunk stages the contiguous change under the cursor, never the working
// file itself. Only ordinary tracked text modifications are supported. The
// captured patch is compared to every displayed change before touching the index.
func (g *Git) StageHunk(path string, displayed []DiffLine, cursor int) error {
	return g.updateIndexHunk(path, displayed, cursor, false)
}

// UnstageHunk reverses the displayed index hunk, preserving the working file.
func (g *Git) UnstageHunk(path string, displayed []DiffLine, cursor int) error {
	return g.updateIndexHunk(path, displayed, cursor, true)
}

func (g *Git) updateIndexHunk(path string, displayed []DiffLine, cursor int, reverse bool) error {
	operation, source := "staging", "unstaged"
	if reverse {
		operation, source = "unstaging", "staged"
	}
	if cursor < 0 || cursor >= len(displayed) ||
		(displayed[cursor].ChangeType != ChangeAdd && displayed[cursor].ChangeType != ChangeRemove) {
		return errors.New("place the cursor on an added or removed line")
	}
	args := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-color", "--no-renames",
		"--src-prefix=a/", "--dst-prefix=b/", "--unified=0", "--inter-hunk-context=0"}
	if reverse {
		args = append(args, "--cached")
	}
	raw, err := g.runGit(append(args, "--", path)...)
	if err != nil {
		return err
	}
	start := strings.Index(raw, "\n@@ ")
	if start < 0 {
		return fmt.Errorf("no %s text hunk; reload the diff", source)
	}
	header := raw[:start+1]
	for _, unsupported := range []string{"new file mode ", "deleted file mode ", "old mode ", "new mode ", " 120000", " 160000"} {
		if strings.Contains(header, unsupported) {
			return fmt.Errorf("hunk %s supports regular tracked text modifications only", operation)
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
		return fmt.Errorf("file changed since display; reload before %s", operation)
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
		args := []string{"apply", "--cached", "--unidiff-zero", "--whitespace=nowarn"}
		if reverse {
			args = append(args, "--reverse")
		}
		cmd := exec.Command("git", append(args, "-")...)
		cmd.Dir, cmd.Env, cmd.Stdin = g.workDir, GitEnv(), strings.NewReader(header+hunk)
		if out, applyErr := cmd.CombinedOutput(); applyErr != nil {
			return fmt.Errorf("hunk %s: %w: %s", operation, applyErr, strings.TrimSpace(string(out)))
		}
		return nil
	}
	return errors.New("hunk not found; reload the diff")
}
