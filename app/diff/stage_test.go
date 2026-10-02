package diff

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGitStageFile(t *testing.T) {
	for _, name := range []string{"notes.txt", ":(glob)*.txt", "space and \"quote\".txt"} {
		t.Run(name, func(t *testing.T) {
			root := setupTestRepo(t)
			old := "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten"
			updated := "one\ninsert A\ninsert B\ntwo\nthree\nfour\nfive\nsix\nseven\nEIGHT\nnine\nten"
			writeFile(t, root, name, old)
			writeFile(t, root, "other.txt", "original other\n")
			gitCmd(t, root, "add", ".")
			gitCmd(t, root, "commit", "-m", "baseline")
			writeFile(t, root, name, updated)
			writeFile(t, root, "other.txt", "unstaged other\n")
			g := NewGit(root)
			require.NoError(t, g.StageFile(name, ""))
			indexed, err := g.runGit("show", ":"+name)
			require.NoError(t, err)
			require.Equal(t, updated, indexed, "all hunks and no-final-newline must be staged")
			other, err := g.runGit("show", ":other.txt")
			require.NoError(t, err)
			require.Equal(t, "original other\n", other, "literal path must not select other files")
			working, err := os.ReadFile(filepath.Join(root, name))
			require.NoError(t, err)
			require.Equal(t, updated, string(working))
		})
	}
}

func TestGitStageFileKinds(t *testing.T) {
	for _, kind := range []string{"untracked", "deleted", "renamed", "binary", "mode", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := setupTestRepo(t)
			writeFile(t, root, "file.txt", "baseline\n")
			gitCmd(t, root, "add", ".")
			gitCmd(t, root, "commit", "-m", "baseline")
			path, oldPath, want := "file.txt", "", "changed\n"
			switch kind {
			case "untracked":
				path = "new.txt"
				writeFile(t, root, path, want)
			case "deleted":
				require.NoError(t, os.Remove(filepath.Join(root, path)))
			case "renamed":
				path, oldPath, want = "renamed.txt", "file.txt", "baseline\n"
				require.NoError(t, os.Rename(filepath.Join(root, oldPath), filepath.Join(root, path)))
			case "binary":
				want = "binary\x00data\xff"
				writeFile(t, root, path, want)
			case "mode":
				gitCmd(t, root, "config", "core.filemode", "true")
				want = "baseline\n"
				require.NoError(t, os.Chmod(filepath.Join(root, path), 0o755))
			case "symlink":
				want = "missing-target"
				require.NoError(t, os.Remove(filepath.Join(root, path)))
				require.NoError(t, os.Symlink(want, filepath.Join(root, path)))
			}
			g := NewGit(root)
			require.NoError(t, g.StageFile(path, oldPath))
			indexed, err := g.runGit("show", ":"+path)
			if kind == "deleted" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, want, indexed)
			}
			if kind == "renamed" {
				_, err = g.runGit("show", ":"+oldPath)
				require.Error(t, err, "rename origin must be removed from the index")
			}
			if kind == "mode" || kind == "symlink" {
				entry, err := g.runGit("ls-files", "--stage", "--", path)
				require.NoError(t, err)
				mode := "100755"
				if kind == "symlink" {
					mode = "120000"
				}
				require.True(t, strings.HasPrefix(entry, mode))
			}
			workingBefore, err := g.runGit("diff", "HEAD", "--binary", "--no-renames")
			require.NoError(t, err)
			require.NoError(t, g.UnstageFile(path, oldPath))
			staged, err := g.runGit("diff", "--cached")
			require.NoError(t, err)
			require.Empty(t, staged)
			// New files become untracked after unstaging; compare their bytes
			// separately because git diff omits untracked paths.
			if kind == "untracked" || kind == "renamed" {
				data, err := os.ReadFile(filepath.Join(root, path))
				require.NoError(t, err)
				require.Equal(t, want, string(data))
			} else {
				workingAfter, err := g.runGit("diff", "HEAD", "--binary", "--no-renames")
				require.NoError(t, err)
				require.Equal(t, workingBefore, workingAfter, "unstaging must not touch working files or modes")
			}
		})
	}
}

func TestGitStageFileRejectsDirectoryAndInvalidPaths(t *testing.T) {
	root := setupTestRepo(t)
	require.NoError(t, os.Mkdir(filepath.Join(root, "folder"), 0o700))
	writeFile(t, root, "folder/child", "must not stage")
	g := NewGit(root)
	for _, path := range []string{"", ".", "folder", "../outside", filepath.Join(root, "folder/child")} {
		require.Error(t, g.StageFile(path, ""))
	}
	require.Error(t, g.StageFile("folder/child", "folder"), "validate both rename paths before staging either")
	indexed, err := g.runGit("ls-files")
	require.NoError(t, err)
	require.Empty(t, indexed)
}

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

func TestGitIndexHunkWithEarlierLineShift(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		for _, addition := range []bool{false, true} {
			for _, earlier := range []string{"first\nearlier A\nearlier B\nalpha\n", ""} {
				t.Run(fmt.Sprintf("reverse=%v/addition=%v/earlier=%q", reverse, addition, earlier), func(t *testing.T) {
					root := setupTestRepo(t)
					before := "start\nfirst\nalpha\nbeta\ngamma\ndelta\nepsilon\nzeta\neta\nend\n"
					target := "gamma"
					selected := strings.Replace(before, "gamma\n", "", 1)
					if addition {
						target = "insert target"
						selected = strings.Replace(before, "gamma\n", "gamma\ninsert target\n", 1)
					}
					after := strings.Replace(selected, "first\nalpha\n", earlier, 1)
					writeFile(t, root, "file.txt", before)
					gitCmd(t, root, "add", ".")
					gitCmd(t, root, "commit", "-m", "baseline")
					writeFile(t, root, "file.txt", after)
					g := NewGit(root)
					update, want := g.StageHunk, selected
					if reverse {
						gitCmd(t, root, "add", ".")
						update = g.UnstageHunk
						want = strings.Replace(before, "first\nalpha\n", earlier, 1)
					}
					lines, err := g.FileDiff(FileDiffRequest{Path: "file.txt", Staged: reverse})
					require.NoError(t, err)
					cursor := slices.IndexFunc(lines, func(l DiffLine) bool { return l.Content == target })
					require.NoError(t, update("file.txt", lines, cursor))
					indexed, err := g.runGit("show", ":file.txt")
					require.NoError(t, err)
					require.Equal(t, want, indexed, "apply only the selected hunk at its original position")
					working, err := os.ReadFile(filepath.Join(root, "file.txt"))
					require.NoError(t, err)
					require.Equal(t, after, string(working))
				})
			}
		}
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

func TestGitUnstageHunk(t *testing.T) {
	for _, name := range []string{"notes.txt", ":(glob)*.txt", "space and \"quote\".txt"} {
		t.Run(name, func(t *testing.T) {
			root := setupTestRepo(t)
			old := "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten"
			staged := "one\ninsert A\ninsert B\ntwo\nthree\nfour\nfive\nsix\nseven\nEIGHT\nnine\nTEN"
			writeFile(t, root, name, old)
			writeFile(t, root, "other.txt", "other original\n")
			gitCmd(t, root, "add", ".")
			gitCmd(t, root, "commit", "-m", "baseline")
			writeFile(t, root, name, staged)
			writeFile(t, root, "other.txt", "other staged\n")
			gitCmd(t, root, "add", ".")
			working := "unrelated unstaged edit\n" + staged
			writeFile(t, root, name, working)
			g := NewGit(root)
			lines, err := g.FileDiff(FileDiffRequest{Path: name, Staged: true})
			require.NoError(t, err)
			cursor := slices.IndexFunc(lines, func(l DiffLine) bool { return l.Content == "insert B" })
			require.NoError(t, g.UnstageHunk(name, lines, cursor))
			indexed, err := g.runGit("show", ":"+name)
			require.NoError(t, err)
			require.Equal(t, "one\ntwo\nthree\nfour\nfive\nsix\nseven\nEIGHT\nnine\nTEN", indexed)
			require.ErrorContains(t, g.UnstageHunk(name, lines, cursor), "changed since display")
			// Remove the remaining hunks in reverse order, including EOF without a newline.
			for _, text := range []string{"TEN", "EIGHT"} {
				lines, err = g.FileDiff(FileDiffRequest{Path: name, Staged: true})
				require.NoError(t, err)
				cursor = slices.IndexFunc(lines, func(l DiffLine) bool { return l.Content == text })
				require.NoError(t, g.UnstageHunk(name, lines, cursor))
			}
			indexed, err = g.runGit("show", ":"+name)
			require.NoError(t, err)
			require.Equal(t, old, indexed)
			other, err := g.runGit("show", ":other.txt")
			require.NoError(t, err)
			require.Equal(t, "other staged\n", other)
			data, err := os.ReadFile(filepath.Join(root, name))
			require.NoError(t, err)
			require.Equal(t, working, string(data))
		})
	}
}

func TestGitUnstageFileInitialAndLiteralPaths(t *testing.T) {
	for _, committed := range []bool{false, true} {
		root := setupTestRepo(t)
		name := ":(glob)*.txt"
		writeFile(t, root, name, "original\n")
		writeFile(t, root, "other.txt", "other\n")
		gitCmd(t, root, "add", ".")
		if committed {
			gitCmd(t, root, "commit", "-m", "baseline")
			writeFile(t, root, name, "staged\n")
			writeFile(t, root, "other.txt", "other staged\n")
			gitCmd(t, root, "add", ".")
		}
		writeFile(t, root, name, "working only\n")
		g := NewGit(root)
		require.NoError(t, g.UnstageFile(name, ""))
		indexed, err := g.runGit("show", ":"+name)
		if committed {
			require.NoError(t, err)
			require.Equal(t, "original\n", indexed)
		} else {
			require.Error(t, err)
		}
		changed, err := g.runGit("diff", "--cached", "--name-only")
		require.NoError(t, err)
		require.Equal(t, "other.txt\n", changed)
		data, err := os.ReadFile(filepath.Join(root, name))
		require.NoError(t, err)
		require.Equal(t, "working only\n", string(data))
	}
}

func TestGitUnstageFileRejectsDirectory(t *testing.T) {
	root := setupTestRepo(t)
	require.NoError(t, os.Mkdir(filepath.Join(root, "folder"), 0o700))
	writeFile(t, root, "folder/child", "keep staged\n")
	gitCmd(t, root, "add", ".")
	g := NewGit(root)
	for _, path := range []string{"", ".", "folder", "../outside"} {
		require.Error(t, g.UnstageFile(path, ""))
	}
	indexed, err := g.runGit("show", ":folder/child")
	require.NoError(t, err)
	require.Equal(t, "keep staged\n", indexed)
}
