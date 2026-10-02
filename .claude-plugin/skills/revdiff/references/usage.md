# Usage

```
revdiff [OPTIONS] [base] [against]
```

## Examples

```bash
revdiff              # review staged, unstaged, and untracked changes
revdiff main         # review changes against a branch
revdiff HEAD~1 HEAD  # review last commit
revdiff main feature # diff between two refs
revdiff main..feature  # same as above, git dot-dot syntax
revdiff main...feature # changes since feature diverged from main
revdiff --only=model.go              # review only files matching model.go
revdiff --only=ui/model.go --only=README.md  # review specific files
revdiff --all-files                  # browse all tracked files (git or jj)
revdiff --all-files --exclude vendor # browse all files, excluding vendor directory
revdiff --include src                # include only src/ files
revdiff --include src --exclude src/vendor  # include src/ but exclude src/vendor/
revdiff main --exclude vendor        # diff against main, excluding vendor
revdiff --only=/tmp/plan.md          # review a file outside a repo (context-only)
revdiff --only=docs/notes.txt        # review a file with no VCS changes (context-only)
revdiff --compare-old=/tmp/plan-old.md --compare-new=docs/plans/plan.md  # diff two arbitrary files (no VCS needed)
printf '# Plan\n\nBody\n' | revdiff --stdin --stdin-name plan.md  # review piped text as markdown
some-command | revdiff --stdin --output /tmp/annotations.txt      # annotate generated output
```

Git working-tree reviews show **Staged** above **Changes** (unstaged and untracked) in the sidebar, separated by a blank row. Each region scrolls independently; partially staged files appear in both with distinct diffs. Help (`?` or `:help`) lists each command once in its functional section, with aliases inline. **Harness** groups connection and message commands; **Miscellaneous** holds ungrouped operations. Keyboard navigation stays in help without typed commands.

## Single-File Mode

Outside Git working-tree reviews, when a diff contains exactly one file, revdiff automatically hides the file tree pane and gives full terminal width to the diff view. Pane-switching keys (`Tab`, `h/l`, `n/p`, `f`, `F`) become no-ops, except when markdown TOC is active (see below). Search navigation (`n`/`N`) still works normally.

## Markdown TOC Navigation

When reviewing a single markdown file in context-only mode (e.g., `revdiff --only=README.md`), a table-of-contents pane appears on the left listing all markdown headers with indentation by level. Use `Tab` to switch between TOC and diff, `j`/`k` to navigate headers, `n`/`p` to jump to next/prev header from either pane, `Enter` to jump to a header. The TOC highlights the current section as you scroll. Headers inside fenced code blocks are excluded.

## All-Files Mode

Use `--all-files` (`-A`) to browse all tracked files, not just diffs. Turns revdiff into a general-purpose code annotation tool. All files shown in context-only mode with full annotation and syntax highlighting support.

- Requires a git or jj repository (uses `git ls-files` / `jj file list` for file discovery; Mercurial is not supported)
- Mutually exclusive with refs and `--only`
- Combine with `--include` (`-I`) to narrow to specific paths and `--exclude` (`-X`) to filter out unwanted paths

```bash
revdiff --all-files                          # all tracked files
revdiff --all-files --include src            # only src/ files
revdiff --all-files --include src --exclude src/vendor  # src/ minus src/vendor/
revdiff --all-files --exclude vendor         # skip vendor/
revdiff --all-files --exclude vendor --exclude mocks  # skip both
revdiff main --exclude vendor                # normal diff, excluding vendor
```

`--include` and `--exclude` can be persisted in config file (`include = src`, `exclude = vendor`) or via env vars (`REVDIFF_INCLUDE=src`, `REVDIFF_EXCLUDE=vendor,mocks`). Include narrows first, then exclude removes from the included set.

## Context-Only File Review

When `--only` specifies a file that has no VCS changes (or when no repo exists), revdiff shows the file in context-only mode: all lines displayed without `+`/`-` markers, with full annotation and syntax highlighting support.

- **Inside a repo (git/hg/jj)**: `--only` files not in the diff are read from disk alongside changed files
- **Outside a repo**: `--only` is required; files are read directly from disk

## Review Description

When launching revdiff for a user (auto-open after a refactor, code review, etc.), include `--description` (or `--description-file=path.md`) to attach prose context to the review. The optional info popup renders the description as markdown. It has no default shortcut or palette command, but users can bind `info`, for example `map alt+i info`. Review statistics appear in the bottom-right status bar. Descriptions support:

- Headings (`#`, `##`) for sections
- Code fences (```` ``` ````) for snippets and commands
- Body prose for the *why* — what the agent did, what the user should look at, open questions

Keep it concise (a few sentences to a small markdown doc). Long content is easier to manage in a file:

```bash
revdiff HEAD~3 --description="# Refactor auth middleware

Drop session-token storage to meet new compliance requirements.
See ticket SEC-441 for context."

# longer markdown lives in a file
revdiff HEAD~3 --description-file=/tmp/review-notes.md
```

`--description` and `--description-file` are mutually exclusive. Both are optional — omit when there's no useful context to add.

## Two-File Diff

Use `--compare-old=<path>` together with `--compare-new=<path>` to diff two arbitrary files on disk using `git diff --no-index`. No VCS repo needed — works anywhere `git` is installed.

- `--compare-old` and `--compare-new` must be used together; both are mutually exclusive with refs, `--only`, `--all-files`, `--stdin`, `--include`, `--exclude`, and `--annotations`
- All standard features work: word-diff, compact mode, syntax highlighting, scrollbar, and inline annotations

```bash
revdiff --compare-old=/tmp/plan-old.md --compare-new=docs/plans/plan.md
revdiff --compare-old=a.txt --compare-new=b.txt
```

## Scratch-Buffer Review

Use `--stdin` to review arbitrary piped or redirected text. revdiff sniffs the input for a git unified-diff signature: when a line beginning with `diff --git a/` is found near the start, the input is parsed as a real multi-file diff (one tree entry per file, with `+`/`-` markers, hunk navigation, word-diff, compact mode, per-file annotations). Otherwise the input is shown as a single context-only buffer — single-file mode, inline annotations, file-level notes, search, wrap, collapsed mode, and structured output all work unchanged.

- `--stdin` is explicit and requires piped or redirected input
- `--stdin-name` sets the synthetic filename used by the context-only buffer (ignored in multi-file diff mode, where real paths are shown)
- `--stdin` conflicts with refs, `--only`, `--all-files`, `--include`, and `--exclude`
- Any per-section parse failure falls the whole input back to raw-text mode so a malformed patch never silently drops files
- Input is capped at 64 MiB

Examples piping a real diff:
- `gh pr diff 123 | revdiff --stdin` — review a GitHub PR end-to-end
- `git format-patch -1 --stdout | revdiff --stdin` — review the latest commit as a multi-file diff

## Key Bindings

**Navigation:**

| Key | Action |
|-----|--------|
| `j/k` or up/down | Navigate files (tree) / scroll diff (diff pane) |
| `h/l` | Switch between file tree and diff pane |
| left/right | Horizontal scroll in diff pane (truncated lines show `«` / `»` overflow indicators at the edges) |
| `Tab` | Switch focus to next pane |
| `PgDown/PgUp` | Page scroll in file tree and diff pane |
| `Ctrl+d/Ctrl+u` | Half-page scroll in file tree and diff pane |
| `J/K` | Scroll diff viewport (works from either pane) |
| `:$` then `Enter` | Jump to the last source line shown in the current file |
| `:` | Open command palette (action name or source line; `Tab` completes, arrows browse) |
| `:<line>` then `Enter` | Jump to a source line in the displayed file (`:1` for the first line; `Esc` cancels) |
| `Enter` | Switch to diff pane (tree) / start annotation (diff pane) |
| `n/p` | Next/previous changed file; next/prev header in markdown TOC mode (n = next match when search active) |
| `P` | Open the file picker |
| `[` / `]` | Cycle through previous/next hunks in the current file (diff focus) or across the tree (tree focus) |
| `:edit` | Open focused file in `$EDITOR` |

The file picker lists paths currently visible in the sidebar, preserving annotated-only and unreviewed-only filters. Printable keys always filter full relative paths; use the arrow keys or mouse wheel to move, and press `Enter` or left-click to jump. `Backspace` edits the filter. The first `Esc` clears a non-empty filter and keeps the picker open; the second closes it. Because printable keys always filter, `P` typed inside the picker adds to the filter rather than closing it; a `jump_file` binding with a modifier (e.g. `map alt+f jump_file`) closes the picker when pressed again.

Press `L` in the file viewer to show line numbers, or launch with `--line-numbers`. Type `:123` and press `Enter` to jump to source line 123; no Vim preset is required. Diff jumps use the new-file line numbers, or old-file numbers when the file is entirely deleted. A line omitted by compact mode or outside the file reports "not shown" and leaves the prompt open for correction. The `:` shortcut is rebindable as `command`.

The command palette runs operations such as `:edit`, `:stage file`, `:stage hunk`, `:reload`, `:set number`, `:help`, or `:quit`, even without a bound key. Cursor movement, scrolling, and next/previous navigation are keyboard actions, not palette commands. Type part of a name or description to filter suggestions, use `↑`/`↓` to browse, then `Tab` to complete. `Enter` runs a complete command, line number, or the sole matching suggestion: both `:set num` and `:set numb` run `:set number` without Tab. Ambiguous and unknown names stay editable; `Esc` cancels. Commands use the focused pane and retain their normal availability checks and confirmations. `:open_editor` opens the annotation at the focused diff line in `$EDITOR`. The bordered pane preserves the footer and works with `--no-status-bar`; commands such as `help` and `quit` also work without a selected file.

Matching prefixes show a muted inline completion: typing `:h` displays `:help` with only `elp` dimmed. Arrow-key browsing changes the suggestion; `Tab` accepts it. Suggestions never change the typed command until accepted.

The palette remembers the last 50 distinct executed commands for the current session. `Ctrl+R` opens a selectable history list, newest first. The input only fuzzy-filters the list, case-insensitively (`dwo` matches `diff words on`). Use `↑`/`↓`, `PgUp`/`PgDn`, or repeat `Ctrl+R` to browse results; `Enter` or `Tab` recalls the selected command without executing it. Press `Enter` again to run it. `Esc` returns to your previous input; `Ctrl+C` closes the palette. History is not saved across restarts.

Annotation commands are grouped under `:annotate`: `:annotate file`, `:annotate hunk`, and `:annotate list`. `:annotate` (or `:a`) annotates the current change hunk when the diff cursor is on one; otherwise it annotates the file, including from tree focus. Explicit hunk annotation requires a changed line. Hunk ranges use the new-code lines for replacements/additions and old-code lines for deletion-only changes. No keyword is needed in the comment. The existing `a`/`Enter` line-annotation shortcuts are unchanged.

`:h` is also an explicit alias for `:help`, so Enter opens help directly. The command palette works from either pane, including while files are loading.

Use `:focus staged` (`:fs`) or `:focus changed` (`:fc`) to focus that section of the split file tree, revealing the tree if hidden and preserving the section's selection. Use `:focus diff` (`:fd`) to focus the diff pane without moving its cursor. From the diff, `Esc` returns to the tree's selected section and file; active prompts, overlays, and search results are dismissed first. A hidden or unavailable tree stays hidden. These commands support completion and unique abbreviations such as `:focus c`. Staged/Changes focus commands are available only in working-tree reviews.

Use `:focus tree` to return to the selected tree section, or `:focus next` to switch panes. Palette commands use readable names such as `:focus diff`. Underscore-style keybinding IDs such as `focus_diff` are for the keybindings file, not command aliases.

Display commands request an explicit state: `:diff context compact` / `:diff context full` select nearby or whole-file context; `:diff removed hide` / `:diff removed show` fold or expand removed lines; `:diff words on` / `:diff words off` control word-change highlighting; and `:tree show` / `:tree hide` control the sidebar. Repeating a command keeps the requested state. Keyboard shortcuts still toggle. Related commands include `:hunk toggle`, `:filter annotated`, `:filter unreviewed`, `:review mark`, and `:theme select`. Use `:files untracked show` / `:files untracked hide` where untracked filtering is available. Split working-tree reviews already include untracked files in Changes.

`.` or `:hunk toggle` shows/hides removed lines in the hunk under the diff cursor, from either pane. From normal view it enables collapsed mode while leaving the other hunks expanded. Repeating it reopens the hunk. Deletion-only hunks collapse to a placeholder. Context lines and addition-only hunks show a hint because there are no removed lines to fold.

Vim-style commands are available too: `:set number` / `:set nonumber` show/hide line numbers, and `:set wrap` / `:set nowrap` enable/disable wrapping. Repeating a `set` command keeps the requested state rather than toggling it. `:q` quits normally, and `:w` flushes annotations to the configured output or connected harness (it does not write source files). These names also support completion.

`:q` refuses to exit while feedback is unsent. Use `:w` to send annotations or `:hs` to reopen a message draft. Successful delivery permits quitting, but failed delivery or subsequent edits still block it. `:q!` discards unsent feedback and quits without emitting annotations. There is no default single-key quit shortcut.

Harness commands support completion too: `:harness connect amp` looks for an Amp session in the current directory without sending anything. `:harness send` (`:hs`) opens a general-message box with no file or line association. `Enter` sends the message; `Esc` closes the box and keeps the draft for this session. Sending a message leaves annotations untouched. Use `O` or `:w` to send annotations. An existing connection stays bound to its original session. Unconfirmed deliveries retain their original text for retry before another message or annotation batch can be sent.

Annotation navigation also has typed commands: `:annotation next` and `:annotation prev`. These commands and `:w` appear in help under **Annotations**. Display settings, including `:blame on` / `:blame off` for the blame gutter, work from either tree or diff focus.

Run `:! <command>` to execute a shell command, for example `:! git status`. It runs through `$SHELL` (or `/bin/sh`) in revdiff's launch directory, with normal quoting, pipes, and terminal input/output. Press Enter when finished to return to the review. Commands are kept in session history with their original case. Annotations are preserved, and `:reload` refreshes the diff after external changes.

Use `:git <args>` as shorthand for `:! git <args>`, for example `:git status` or `:git commit -m "Fix parsing"`. Arguments retain their case and shell quoting.

Each command opens a clean screen without exposing or clearing your shell's scrollback. Use `:!!` to rerun the last shell command in this session, even after other palette commands.

With the diff focused, `:blame view` (or `:bv`) opens attribution for the selected line: commit, author, date, and summary. Removed lines use the old side of the diff. This opens details without changing the gutter. For a github.com `origin`, it also shows a commit URL and looks up associated merged PRs using authenticated `gh` (optional, five-second timeout). Local blame still works when PR lookup is unavailable. Uncommitted lines have no commit or PR link.

Single-line annotation, search, command, file-picker, and theme-picker inputs use revdiff's built-in Bubbles text-input bindings, not your shell's keymap: arrows and `Ctrl+B`/`Ctrl+F` move by character, `Ctrl+A`/`Ctrl+E` move to the start/end, `Backspace`/`Ctrl+H` delete backward, `Ctrl+W` (or `Alt+Backspace`) deletes the previous word, and `Ctrl+U`/`Ctrl+K` delete from the cursor to the start/end. Existing `Enter`, `Esc`, search-history, and list-navigation behavior is unchanged. The terminal sends key sequences; revdiff cannot inherit zsh/readline bindings or Command-key shortcuts. Configure Option/Alt to send Meta for Alt bindings.

**Search:**

Press `/` or run `:search` to open search in the command palette. With the diff pane focused, search and `n`/`N` stay within the current file. With the file tree focused, they search file contents across the tree in tree order, respecting active tree filters and diff modes. Both directions wrap around. Switching focus changes the scope without replacing the query; the match counter shows the position within the displayed file.

The `/` prompt and search-history help appear above the footer, including with `--no-status-bar`. Tree searches are labeled “Search file tree”. `Enter` finds a match and closes the pane; `Esc` or `Ctrl+C` cancels without replacing the previous search. The status bar keeps its normal file and navigation information while you type.

| Key | Action |
|-----|--------|
| `/` | Search current file (diff focus) or files in the tree (tree focus) |
| `n` | Next search match (overrides next file when search active) |
| `N` | Previous search match |
| `↑` / `Ctrl+P` | Recall previous search query (in search prompt) |
| `↓` / `Ctrl+N` | Recall next search query / clear (in search prompt) |
| `Esc` | Cancel search input / clear search results |

**Annotations:**

| Key | Action |
|-----|--------|
| `a` or `Enter` (diff pane) | Annotate current diff line |
| `A` | Add file-level annotation (stored at top of diff) |
| `@` | Toggle annotation list popup (navigate and jump to any annotation) |
| `}` / `{` | Jump to next/previous annotation (always crosses file boundaries; silent no-op at the first/last annotation) |
| `d` / `:annotation delete` | Delete annotation under cursor |
| `O` | Send feedback with `--amp`, or export via `--output` / `--post-flush-command` |
| `s` | Stage/unstage change under cursor (modified tracked text, Git working tree) |
| `S` (Shift+S) | Stage/unstage entire selected file (Git working tree, `stage_file` — rebindable) |
| `Esc` | Cancel annotation input |

In **Changes**, `s` / `S` stage a hunk / file (`:stage hunk` / `:stage file`). In **Staged**, the same keys unstage (`:unstage hunk` / `:unstage file`). Hunk operations support modified tracked regular text files and reject stale views. Whole-file staging includes working-copy edits since the last render; unstaging restores the index to HEAD, including removing initial additions. Whole-file operations support new/deleted files, renames, binaries, symlinks, and mode changes. They return focus from the diff to the next file in the same tree section. Both shortcuts require a Git working-tree review and no pending annotations on the selected file or its rename origin; neither changes working files. Annotations on other files, including unconfirmed sends, remain intact and do not block staging or unstaging; an active send must finish first.

The optional `open_editor` action hands annotation text to an external editor for multi-line comments. It has no default shortcut; bind it explicitly in the keybindings file if desired. Text inputs retain their normal editing bindings. Editor resolution: `$EDITOR` → `$VISUAL` → `vi`. Values with arguments work (e.g. `EDITOR="code --wait"`). On editor save and quit, the full file contents (including newlines) become the annotation. Quitting the editor with an empty file cancels the annotation and preserves any previously stored note on that line. Multi-line annotations are rendered line-by-line in the diff view, shown flattened in the annotation list popup (`@`), and emitted with embedded newlines in the structured output.

**Code inspection:** From a current source line in diff focus, use `:inspect hover`, `:inspect definition`, or `:inspect references`. Select an identifier, then inspect its documentation or open a read-only location preview. Esc returns to the unchanged review. Press `:` from a text/result popup to open the command palette. Requires a server on PATH: `gopls` (Go), `typescript-language-server --stdio` (TypeScript/JavaScript with compatible TypeScript), `pyright-langserver --stdio` (Python), or `rust-analyzer` (Rust). Run `:lsp install go`, `:lsp install typescript`, `:lsp install python`, or `:lsp install rust` to explicitly install through the shell view using Go, npm, or rustup. Add the install directory to PATH before starting revdiff. Servers start only on demand. Inspection never applies edits or installs automatically. It supports current working-tree/all-files/standalone files, not staged/historical/stdin/compare views or removed lines. Changed source lines require a reload. Esc cancels queries, which time out after 30 seconds.

Hover code blocks and source previews use the active syntax-highlighting theme. Hover hides code fences and preserves the surrounding documentation. Unknown languages remain readable as plain text.

`:lsp list` shows supported languages, server executables, PATH availability, and install commands without starting servers.

Run `:edit` to open the focused file in `$EDITOR` (`open_file_in_editor` — optionally bindable, with no default key) when revdiff has a stable source path. From the file tree, this opens the selected file without a line target; from the diff, it uses the focused source line. Editing from the **Staged** tree or a staged diff reports an error; select the file in **Changes** instead. Editor resolution is the same `$EDITOR` → `$VISUAL` → `vi` chain. Known editors receive either `$EDITOR +N path` or `$EDITOR --goto path:N` as appropriate; unknown editors receive only the file path. File lines are resolved on a best-effort basis. For working tree changes, a clean editor exit reloads the selected file. For refs, a clean editor exit returns to revdiff without reloading the displayed diff. In compare mode, `:edit` opens the `--compare-new` side. Working tree files with line annotations cannot be opened for editing because edits can orphan those annotations. Diffs read with `--stdin` do not support opening files. Unsupported rows or files and editor errors show a status hint instead of launching an editor or changing the diff.

For Amp live review, run **revdiff: connect** in Amp and launch `revdiff --amp CONNECTION_FILE` in the other terminal pane. Both must use the same host and Git checkout. `O` sends annotations to the selected thread and clears only acknowledged, unchanged comments. Automatic refresh pauses during drafts and while unsent comments remain. Failed sends keep the pending snapshot; `O` retries it. `s` (`stage_hunk`) stages the contiguous change under the cursor; it rejects stale views and unsupported file types. Quit does not send feedback. This requires this fork's binary, not an upstream release, and cannot be combined with refs, staged/all-files/stdin/compare modes, output, or post-flush commands.

Outside Amp mode, press `O` to export the current annotations without exiting (`flush_output`, rebindable). Configure `--output`, `--post-flush-command`, or both. With `--output`, each flush atomically overwrites the file with the full current annotation set. With `--post-flush-command`, the same snapshot is sent to the command on stdin. If neither is configured, or if there are no annotations, revdiff shows a status hint and does nothing.

For clipboard-only flushes on macOS, set `post-flush-command = pbcopy` in `~/.config/revdiff/config`; no `--output` flag is required. On Linux, use `xclip -selection clipboard` for X11 or `wl-copy` for Wayland. For remote terminal clipboards, configure an OSC 52 helper as the post-flush command.

Press `Space` to mark the focused file reviewed. Press `F` to toggle the sidebar between all files and unreviewed files; while filtered, marking a file reviewed removes it from the list and advances to the next unfinished file. On `R` reload, revdiff keeps the mark only when the file's effective text diff is unchanged; rebases that only shift line numbers or surrounding context keep it, while changed or removed files lose it. Binary files and opaque placeholders are conservatively unmarked on reload because their rendered diff does not expose enough content to prove they are unchanged.

**View:**

| Key | Action |
|-----|--------|
| `v` | Toggle collapsed diff mode (shows final text with change markers) |
| `C` | Toggle compact diff view (small context around changes, re-fetches current file) |
| `w` | Toggle word wrap (long lines wrap with `↪` continuation markers) |
| `t` | Toggle tree/TOC pane visibility (gives diff full terminal width) |
| `L` | Toggle line numbers (side-by-side old/new for diffs, single column for full-context files) |
| `B` | Toggle blame gutter (author name + commit age per line) |
| `W` | Toggle intra-line word-diff highlighting for paired add/remove lines |
| `.` | Show/hide removed lines in the hunk under the diff cursor |
| `T` | Open theme selector with live preview |
| `f` | Toggle filter: all files / annotated only |
| `F` | Toggle filter: all files / unreviewed only |
| `?` | Toggle help overlay showing all keybindings |
| `R` | Reload diff from VCS (warns if annotations exist) |
| `:q` | Quit after sending feedback; `:q!` discards unsent feedback |

## Status Bar Icons

The status bar shows a fixed row of mode indicators on the right side. All slots are always rendered — active modes use the status bar foreground color, inactive modes use muted gray, so the row occupies the same width regardless of what's toggled on. The help overlay (`?`) shows each icon beside the key that controls it.

| Icon | Toggle | Meaning |
|------|--------|---------|
| `▼` | `v` | Collapsed diff mode |
| `⊂` | `C` | Compact diff mode (small context around changes) |
| `◉` | `f` | Filter: annotated files only |
| `↩` | `w` | Word wrap mode |
| `≋` | `/` | Search active |
| `⊟` | `t` | Tree/TOC pane hidden (diff uses full width) |
| `#` | `L` | Line numbers visible in gutter |
| `b` | `B` | Blame gutter visible |
| `±` | `W` | Intra-line word-diff highlighting |
| `✓` / `○` | `Space` / `F` | Reviewed files / unreviewed-only filter active |
| `∅` | `u` | Untracked files visible in tree |

On narrow terminals, the left-hand segments are dropped before the icons: search position first, then line and hunk info, then the filename truncates. The icon row on the right stays put.

## Mouse Support

revdiff enables mouse tracking by default so the scroll wheel and left-click work consistently across terminals.

- **Scroll wheel** — scrolls whichever pane the cursor is over. In the tree/TOC pane the wheel moves the cursor one entry per notch (matches `j`/`k`). In the diff pane the wheel scrolls the viewport by three lines per notch — the diff cursor stays on its current logical line and is pinned to the visible edge if scrolling pushes it off-screen.
- **Shift+scroll** — half-page scroll in the diff pane. In the tree/TOC pane Shift+wheel behaves the same as plain wheel (one entry per notch — no page step).
- **Left-click in the tree** — focuses the tree and selects/loads the clicked entry. Clicking a directory row moves the cursor but does not load a file.
- **Left-click in the diff** — focuses the diff and moves the cursor to the clicked line. Enables a "click, then `a`" annotation flow.
- **Left-click in the TOC pane** (single-file markdown) — focuses the TOC and selects the clicked header.
- **Scroll wheel in overlay popups** (info, annotations, themes, help) — scrolls the popup content or moves its cursor. Shift+wheel uses a half-page step. In the theme selector, wheel previews each theme live.
- **Left-click in the annotation popup** — jumps to the clicked annotation (same as pressing `Enter`).
- **Left-click in the theme popup** — confirms the clicked theme (same as pressing `Enter`). Clicks on the filter row or blank separator are ignored.
- **Left-click in the file picker** — jumps to the clicked file (same as pressing `Enter`). Clicks on the filter row or blank separator are ignored.
- **Scroll wheel in the file picker** — moves the picker cursor. Shift+wheel uses a half-page step.

Horizontal wheel, right-click, middle-click, drag selection, and clicks on the status bar or diff header are intentionally ignored. Clicks outside an open overlay are swallowed — dismiss an overlay with `Esc` or its toggle key. Modal states (annotation input, search input, reload confirm) swallow mouse events entirely.

**Text selection trade-off** — once mouse tracking is on, plain drag is captured by revdiff. For terminal-native text selection:

- **kitty**: hold `Ctrl+Shift` while dragging
- **iTerm2**: hold `Option` while dragging
- **ghostty** (and ghostty-based terminals such as agterm): hold `Shift` while dragging. Ghostty also uses `Shift` to *extend* an existing selection, so if text is already selected the drag grows that selection instead of starting a new one - clear it first. Ghostty 1.3.0+ additionally has a `toggle_mouse_reporting` keybind, unbound by default, which suspends mouse capture without restarting revdiff
- **most other terminals**: hold `Shift` while dragging

Because the tree pane is rendered alongside the diff on the same rows, multi-line Shift+drag will include tree content. For clean copies of diff text, use your terminal's block-select mode (Option+drag in iTerm2, Ctrl+Shift+drag in kitty) or run with `--no-mouse` to disable mouse capture entirely.

Opt out with `--no-mouse`, `REVDIFF_NO_MOUSE=true`, or `no-mouse = true` in the config file.

## Custom Keybindings

All keybindings can be customized via `~/.config/revdiff/keybindings` (override path with `--keys` or `REVDIFF_KEYS`).

```
# map <key> <action> — bind a key
# unmap <key> — remove a default binding
map x quit
unmap q
map ctrl+d half_page_down
```

Generate a template with all defaults: `revdiff --dump-keys > ~/.config/revdiff/keybindings`

**Chord bindings (ctrl/alt leader):** bind a two-stage chord by joining the leader and second key with `>`. The leader must be a `ctrl+*` or `alt+*` combo; the second stage is any single key. Only two stages are supported.

```
map ctrl+w>x mark_reviewed
map alt+t>n theme_select
```

When the leader is pressed, the status bar shows `Pending: ctrl+w, esc to cancel`; press the second key to dispatch, or `esc` to cancel silently. Binding a key as both a standalone action and a chord prefix drops the standalone binding (the chord wins, with a warning). Chord bindings work under non-Latin keyboard layouts — the second-stage key is translated via the same layout-resolve fallback as single-key bindings.

**macOS note:** `alt+*` leaders require your terminal to send Option as Meta/Alt. Most terminals default to "Option composes special characters" (e.g. `Option+T` → `†`), in which case Alt chords silently won't fire. To enable: iTerm2 → *Profiles → Keys → Left/Right Option key → `Esc+`*; Terminal.app → *Profiles → Keyboard → Use Option as Meta key*; Kitty → `macos_option_as_alt yes`; Ghostty → `macos-option-as-alt = true`. If you'd rather not touch terminal settings, use `ctrl+*` leaders — those work everywhere with no configuration.

See the [configuration reference](config.md) for the full list of available actions.

## Vim-motion Preset

Opt-in vim-style motion layer activated via `--vim-motion`, `REVDIFF_VIM_MOTION=true`, or `vim-motion = true` in the config file. Off by default — when off, existing single-key bindings are unchanged.

Cursor counts such as `5j` and `12k` work in the file pane **without this preset**. They repeat normal cursor movement, including annotation rows and skipping hidden lines, and stop at file boundaries. Counts range from 1 to 9999; `Esc` cancels a pending count. Without the preset, counts follow your up/down bindings (including arrows), explicit digit bindings take precedence, and digits in text inputs remain text.

| Keys | Action |
|------|--------|
| `<N>j` / `<N>k` | Move cursor N lines down/up (diff pane, 1-9999) |
| `gg` | Jump to first line (diff pane) |
| `G` | Jump to last line (diff pane) |
| `<N>G` | Goto line N (diff pane) |
| `H` / `<N>H` | Cursor to top of screen / Nth line from top (diff pane) |
| `M` | Cursor to middle of screen (diff pane) |
| `L` / `<N>L` | Cursor to bottom of screen / Nth line from bottom (diff pane) |
| `zz` | Center viewport on cursor (diff pane) |
| `zt` | Align viewport top on cursor (diff pane) |
| `zb` | Align viewport bottom on cursor (diff pane) |
| `ZZ` | Quit (any pane) |

When the preset is on, the digits `0`-`9` and the leader keys `g`, `z`, `Z` are intercepted before the regular keymap, so any standalone binding on those keys is overridden while the flag is active. `<N>j`/`<N>k`/`<N>G`, `gg`/`zz`/`zt`/`zb`, and the screen-position motions `H`/`M`/`L` apply to the diff pane only — in the file tree they fall through to the normal bindings. `ZZ` works from any pane. While the preset is active `L` is a screen-position motion rather than the line-numbers toggle (`toggle_line_numbers`); remap it in the keybindings file if you need both. Press `Esc` to silently cancel a pending leader; an unknown second key surfaces a transient `Unknown: <chord>` hint in the status bar. A bare digit `0` is not consumed; counts over 9999 are clamped. Modal keys (search input, annotation input, overlay navigation) always take precedence over the interceptor, and `ctrl+*`/`alt+*` chord bindings keep working orthogonally.

The help overlay (`?`) shows a dedicated **Vim motion** section listing all ten preset bindings when `--vim-motion` is on; when off, the section is hidden.

## Output Format

On quit, revdiff outputs annotations to stdout:

```
## handler.go (file-level)
consider splitting this file into smaller modules

## handler.go:43 (+)
use errors.Is() instead of direct comparison

## handler.go:43-67 (+)
refactor this hunk to reduce nesting

## store.go:18 (-)
don't remove this validation
```

Each annotation block: `## filename:line[-end] (type)` where type is `(+)` added, `(-)` removed, or `(file-level)`. The `-end` suffix is included when the annotation covers a line range.

When annotation text contains the keyword "hunk" (case-insensitive, whole word), the output header automatically expands to include the full hunk line range (e.g., `handler.go:43-67 (+)` instead of `handler.go:43 (+)`). This gives AI consumers the range context without any extra steps.

Comment body lines starting with `## ` (the record-header form) are prefixed with a single space on output so parsers that split on `## ` record headers cannot confuse a multi-line comment for a new record.

Use `--output` / `-o` flag to write annotations to a file instead of stdout.

Exit status: `0` = no annotations or default mode; `10` = annotations were produced with `--exit-code-on-annotations`, `REVDIFF_EXIT_CODE_ON_ANNOTATIONS`, or `exit-code-on-annotations`; `1` = real errors. Agent launchers set `REVDIFF_EXIT_CODE_ON_ANNOTATIONS` and treat `10` as success-with-annotations.

## Asking Questions Instead of Directives

An annotation is normally an instruction to change code. To ask about the code instead, put `??` anywhere in the text, or open with `explain`, `remind`, `describe`, `what is`, `what are`, `how does`, `how do` or `clarify` (case-insensitive). `??` is the language-neutral form and works whatever language you write in.

```
## renderer.go:142 (+)
why a pointer here??

## store.go:88 (-)
explain what this lock protects
```

The agent answers as a markdown document and reopens it in revdiff via `--only`, with a TOC sidebar. Annotate that document to ask follow-ups and it is refined and reopened; the loop ends when you quit without annotating. Code-change annotations from the same batch are held and applied after the explanation loop finishes. Applies to the Claude and Codex plugins; the Pi package classifies questions the same way but answers them in chat.

## Preloading Annotations

Use `--annotations=PATH` to preload the annotation store from a markdown file in the same `-o` format. The format is bidirectional: any file written by `-o` can be read back via `--annotations` for round-trip workflows — review, quit, edit the file externally, relaunch, and continue from the preloaded state.

## Review History

When you quit with annotations (`q`), revdiff automatically saves a copy of the review session to `~/.config/revdiff/history/<repo-name>/<timestamp>.md`. This is a safety net — if annotations are lost (process crash, agent fails to capture stdout), the history file preserves them.

Each history file contains:
- Header with path, refs, and (git only) a short commit hash
- Full annotation output (same format as stdout)
- Raw git diff for annotated files only

History auto-save is always on and silent — errors are logged to stderr, never fail the process. No history is saved when there are no annotations. For `--stdin` mode, files are saved under `stdin/` subdirectory; for `--only` without git, the parent directory name is used instead of a repo name.

The history file is also a crash-recovery save when the process is terminated by a signal — a SIGHUP from a dropped SSH or tmux client, or a SIGTERM. On a signal exit only the history file is written, never the `-o` output, because a signal is not the deliberate handoff that `q` and `O` perform. Recover the annotations the usual way — load the newest history file. A signal-delivered SIGTERM previously wrote the `-o` output; it no longer does.

Override the history directory with `--history-dir`, `REVDIFF_HISTORY_DIR` env var, or `history-dir` in the config file.

## Disconnect-Resilient Window Mode (tmux)

Set `REVDIFF_TMUX_WINDOW=1` in the launcher's environment to open revdiff in a persistent, server-owned tmux window instead of a client-owned `display-popup`. A dropped SSH or tmux client tears down a popup and kills the review, but a server-owned window survives the disconnect — reattach and the live review is still there. This is a launcher environment variable, not a revdiff flag.

## Pane-Scoped Overlay (herdr)

Set `REVDIFF_HERDR_PANE=1` in the launcher's environment to open revdiff in a zoomed split of the agent's own herdr pane instead of a new fullscreen tab, so the agent pane stays one keypress away. It needs a herdr whose CLI carries `pane split`, `pane get` and `pane close`; an unsupported CLI or a refused split falls back to the tab overlay; a split that succeeds but returns no usable pane id fails closed with a warning rather than opening a second surface, and may leave a stray pane to close by hand. This is a launcher environment variable, not a revdiff flag.

## Pane-Scoped Overlay (agterm)

Set `REVDIFF_AGTERM_PANE=1` in the launcher's environment to open revdiff in the agent's own split pane instead of over the whole session, leaving the sibling pane live and visible. It applies only when that session is split — the session-wide overlay stands otherwise, and the launcher retries session-wide if agterm refuses the pane. The review gets pane width rather than session width, which is why it is opt-in. This is a launcher environment variable, not a revdiff flag.
