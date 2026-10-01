# Amp plugin

This directory is the source of truth for revdiff's [Amp](https://ampcode.com) plugin, installer, and tests. No separate plugin repository is needed. The Go transport client compiled into revdiff lives in [app/harnesses/amp](../../app/harnesses/amp/README.md).

## revdiff: split-terminal live review

`revdiff.ts` connects revdiff in one terminal pane to a specific Amp thread in another. It does not create panes or spawn agents.

### Install

From the revdiff repository root:

```sh
./plugins/amp/install.sh
```

The installer copies only `revdiff.ts` to `~/.config/amp/plugins/revdiff.ts` for use across local repositories. It works from any working directory when invoked by its path. It leaves other plugins untouched and refuses to replace a different installed version unless you pass `--force`. Rerun `./plugins/amp/install.sh --force` after updating this checkout to update the installed copy; review or save any local customizations first. Symlinks and non-regular destinations are always refused.

Reload Amp plugins or restart Amp after installation or updates. Amp does not automatically discover `plugins/amp/`. Alternatively, copy `revdiff.ts` into a project's `.amp/plugins/` directory for that project only. Install one copy, not both.

The matching revdiff binary must include `--amp`, live refresh, and hunk staging. Build the [revdiff fork](https://github.com/cmattatall/revdiff) with `make build` and add its `.bin` directory to your PATH. Upstream revdiff releases do not provide this integration. The binary-side implementation must be present in your checkout; publishing this plugin alone does not release that binary.

### Review

1. Open an Amp thread in the Git checkout you want to review.
2. Press **Ctrl+O** and choose **revdiff: connect** in Amp's command palette. The plugin posts the launch command as a persistent thread message, not a popup. Amp may acknowledge this setup message; it explicitly tells the agent not to execute the command.
3. Copy the command from that message into the sibling terminal pane, in the same checkout. It includes the real connection-file path. If using the build from this checkout, replace the initial `revdiff` executable with `./.bin/revdiff`.
4. Annotate a line with `Enter` or `a`, annotate a file with `A`, and inspect annotations with `@`.
5. Press `O` to send feedback without quitting revdiff. The plugin appends it to the bound Amp thread as a steering message, including while the agent is working.
6. Press `s` on an added or removed line to stage that contiguous change. The user owns staging; feedback tells Amp not to stage, unstage, reset, or commit without asking.

Revdiff refreshes the file list and selected diff about once per second. Refresh pauses during annotation input, while unsent comments exist, and during modal interactions. Successful sends clear only delivered comments that have not changed during delivery. Edited or new comments remain for the next send. Quit does not send: remaining annotations follow revdiff's usual output/history behavior.

Choose **revdiff: disconnect** when finished. Reconnecting the same thread reuses its active connection and posts its command again; different threads have separate servers and connection files. Old messages remain in the transcript, but their commands stop working after disconnect or plugin reload. Connect again for a current command.

### Safety and limitations

- Amp and revdiff must run on the same host and filesystem. A remote orb cannot connect to a local terminal using this transport.
- The plugin listens only on loopback after an explicit connect command. It authenticates feedback with a random token stored in a private temporary connection file, rejects browser-origin requests, and limits request size to 1 MiB. Treat connection files as credentials; do not commit or share them.
- Requests are acknowledged only after Amp accepts the message. Press `O` after a transport failure to retry the same snapshot and request ID. Concurrent and repeated IDs are deduplicated within that connection.
- If Amp fails to append feedback, the plugin caches the uncertain outcome rather than risking duplicate side effects. Check the thread before disconnecting/reconnecting and relaunching revdiff to resend. Deduplication does not survive restart or crash.
- Graceful disconnect or plugin disposal removes private connection files and closes servers. After restart, reconnect and relaunch revdiff.
- `--amp` requires an unstaged Git working-tree review. Do not combine it with refs, `--staged`, `--all-files`, stdin/compare modes, `--output`, or `--post-flush-command`.
- Hunk staging supports modified tracked regular text files, not new/deleted files, renames, binaries, or mode changes. It rejects stale displayed changes and updates only the index, never the working file.

### Tests

From the revdiff repository root, with Node 22.18+ (native TypeScript support):

```sh
node --test plugins/amp/tests/*.test.ts
```

No dependency installation is needed. Tests use real loopback HTTP with a mocked Amp thread API and cover thread isolation, authentication, input bounds, request deduplication, uncertain failures, and cleanup. Installer tests use a temporary home directory and check installation, replacement protection, and paths containing spaces. Go client, live-refresh, and hunk-staging tests run with `make race`.
