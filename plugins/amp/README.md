# Amp plugin

This directory is the source of truth for revdiff's [Amp](https://ampcode.com) plugin, installer, and tests. No separate plugin repository is needed. The Go transport client compiled into revdiff lives in [app/harnesses/amp](../../app/harnesses/amp/README.md).

## revdiff: split-terminal live review

`revdiff.ts` connects revdiff in one terminal pane to a specific Amp thread in another. It does not create panes or spawn agents.

### Install

From the revdiff repository root:

```sh
./plugins/amp/install.sh
```

The installer requires Go. It builds this checkout's binary into `~/.local/bin/revdiff` and copies `revdiff.ts` to `~/.config/amp/plugins/revdiff.ts` for use across local repositories. It works from any working directory when invoked by its path. It replaces the installed binary and plugin automatically, including any local customizations, and leaves other plugins untouched. Rerun `./plugins/amp/install.sh` after updating this checkout. Symlinks and non-regular binary/plugin destinations are always refused.

Shell detection is automatic, using `$SHELL`. For bash, the installer adds a guarded PATH block to `.bashrc` and the first existing login profile (`.bash_profile`, `.bash_login`, `.profile`), creating `.bash_profile` if none exists. For zsh it updates `${ZDOTDIR:-$HOME}/.zshrc`. Repeated installs do not duplicate the block. Use `--shell bash|zsh` only to override detection, or `--no-path` to leave startup files alone. Unsupported shells get manual instructions. Open a new terminal afterward, or run `export PATH="$HOME/.local/bin:$PATH"` in your current bash/zsh session.

Reload Amp plugins or restart Amp after installation or updates. Amp does not automatically discover `plugins/amp/`. Alternatively, copy `revdiff.ts` into a project's `.amp/plugins/` directory for that project only. Install one copy, not both.

The installer builds the matching binary from this [revdiff fork](https://github.com/cmattatall/revdiff). For development without installation, use `make build` and run `./.bin/revdiff`. Upstream revdiff releases do not provide this integration. Publishing this plugin alone does not release that binary.

### Review

1. Open an Amp thread in the Git checkout you want to review. The plugin registers the session silently. When reloading it in an already-open thread, send a prompt or choose **revdiff: connect** to register it.
2. Run `revdiff` in a sibling terminal pane in the **same directory** as Amp's workspace. Directory symlinks are resolved; a parent, child, sibling, or another Git worktree does not match. Exactly one live match connects automatically. If none matches, revdiff keeps checking once per second, even during annotation input; `O` also retries discovery. Type `:harness connect amp` to retry manually without sending annotations. You can start revdiff before Amp registers without restarting it.
3. Annotate a line with `Enter` or `a`, annotate a file with `A`, and inspect annotations with `@`.
4. Press `O` (Shift+O) or run `:w` to send annotations without quitting revdiff. Use `:harness send` (`:hs`) to compose a general message without annotating a file. `Enter` sends the message and `Esc` retains its draft. Both use the same harness interface. The plugin appends feedback to the bound Amp thread as a steering message, including while the agent is working. Help groups connection and message commands under **Harness**, with aliases on the same row.
5. In the **Changes** region, press `s` on an added or removed line to stage that contiguous change, or `S` (Shift+S) to stage the entire selected file from the tree or diff pane. The user owns staging; feedback tells Amp not to stage, unstage, reset, or commit without asking.

The bottom panel shows **Harness: waiting** before connecting, then **Harness (amp): &lt;title&gt; &lt;thread ID&gt;**; the identity replaces a separate "connected" label. Each harness supplies its own name and display text. **sending** and **unconfirmed** appear beside that identity during a send or after an unacknowledged delivery. **Harness: unavailable** means discovery failed (for example, multiple sessions matched); press `O` for details. The row stays visible during annotation input and transient status messages, unless the status bar is disabled. A background connection never sends annotations on its own; `O` or `:w` is still required. Without a connection, comments remain local.

The title is a snapshot taken when the plugin registers the connection; untitled threads show the ID alone. Long titles shorten before the ID. The panel stays visible during annotation input and late connection discovery without moving the diff. `--no-status-bar` hides it.

Revdiff refreshes the file list and selected diff about once per second. Refresh pauses during annotation input, while unsent comments exist, and during modal interactions. Successful sends clear only delivered comments that have not changed during delivery. Edited or new comments remain for the next send. `:q` refuses to exit with unsent annotations or a message draft. Send feedback first, or use `:q!` to explicitly discard it and quit. Quit never sends feedback to the harness.

Choose **revdiff: disconnect** when finished. This also disables automatic registration for that thread until manual reconnect or plugin reload. Reconnecting the same thread reuses its active connection and posts its command again; different threads have separate servers and connection files. Old messages remain in the transcript, but their commands stop working after disconnect or plugin reload. Relaunch revdiff after reconnecting; an existing review stays bound to its original connection and never silently switches threads.

### Safety and limitations

- Amp and revdiff must run on the same host, filesystem, and user account. A remote orb cannot connect to a local terminal using this transport.
- The plugin listens only on loopback when a thread session opens (or starts an agent turn), or after a manual connect command. It authenticates feedback with a random token stored in a private connection file under `~/.cache/revdiff/amp`, rejects browser-origin requests, and limits request size to 1 MiB. Treat connection files as credentials; do not commit or share them.
- Discovery probes matching registrations with an authenticated, read-only request that checks the thread and directory. It ignores dead, malformed, or mismatched registrations. Multiple live connections, even to the same thread, require explicit selection because each has its own delivery retry cache.
- Requests are acknowledged only after Amp accepts the message. Press `O` after a transport failure to retry the same snapshot and request ID. Concurrent and repeated IDs are deduplicated within that connection.
- If Amp fails to append feedback, the plugin caches the uncertain outcome rather than risking duplicate side effects. Check the thread before disconnecting/reconnecting and relaunching revdiff to resend. Deduplication does not survive restart or crash.
- Graceful disconnect or plugin disposal removes private connection files and closes servers. A crash may leave stale files, which discovery ignores without deleting. After restart, register the session and relaunch revdiff.
- Automatic connection applies to Git working-tree reviews. Refs, `--all-files`, stdin/compare modes, `--output`, and `--post-flush-command` skip discovery; combining them with explicit `--amp` is an error.
- Hunk staging supports modified tracked regular text files, not new/deleted files, renames, binaries, or mode changes. It rejects stale displayed changes and updates only the index, never the working file.
- Whole-file staging (`stage_file`, default `S`) stages the current working-tree version like `git add`, including edits since the last render. It supports new/deleted files, renames, binaries, symlinks, and mode changes, but not directories or submodules. Both staging shortcuts require no pending annotations and a Git working-tree review; neither changes the working file.

### Tests

From the revdiff repository root, with Node 22.18+ (native TypeScript support):

```sh
node --test plugins/amp/tests/*.test.ts
```

No dependency installation is needed. Tests use real loopback HTTP with a mocked Amp thread API and cover automatic registration, disconnect suppression, thread isolation, authentication, input bounds, request deduplication, uncertain failures, and cleanup. Installer tests use disposable home directories and a fake Go compiler to check shell detection, profile selection, installation, automatic replacement, symlink protection, build failure, and paths containing spaces. Go discovery, client, live-refresh, and hunk-staging tests run with `make race`.
