# Amp harness client

This package is the revdiff side of the live Amp integration, not an Amp plugin.
The plugin, its installer, and its tests live in
[plugins/amp](../../../plugins/amp/README.md) in this repository.

## Why this lives outside `app/ui`

`app/ui` owns review interactions: collecting annotations, showing delivery status,
retaining unacknowledged feedback, and refreshing the diff. It should not own
filesystem access, HTTP requests, credentials, or a particular harness protocol.

This package owns those external concerns: reading the private connection file,
checking that it belongs to the reviewed repository, sending authenticated
loopback HTTP requests, and preserving request IDs across retries. A successful
send means the plugin acknowledged appending the feedback to the selected thread.

`Discover` reads private registrations under `~/.cache/revdiff/amp`, matches the
exact current directory (resolving symlinks), and probes the authenticated endpoint
without posting feedback. No live match returns no client; multiple live matches
require explicit `--amp` selection. Malformed or stale registrations are ignored,
not removed. The composition root gates discovery to compatible Git working-tree
reviews; explicit `--amp` takes precedence. Once selected, clients never switch threads.

The composition root in `app/revdiff/main.go` constructs `amp.Client` and injects
it through `ui.ModelConfig.Feedback`. It also supplies `DiscoverFeedback` for
automatic mode, so the UI can asynchronously retry discovery on a one-second timer
or an `O` press until a client is bound. Discovery does not pause for unsent drafts,
but file refresh still does. The status bar reports connection and delivery state.
The bottom panel shows `Harness (amp): <title> <thread ID>`, alongside the
repository root supplied through `ReviewInfoConfig`. The client formats its cached
title and thread ID as display text; untitled threads show the ID alone. Titles are
snapshots from plugin registration. Long display text is shortened from the left.
The UI declares the small `FeedbackSender` interface (`Send(content string) error`,
`HarnessName() string`, and `DisplayName() string`); it does not import this package
or interpret harness-specific identity fields. The name methods perform no IO,
so rendering cannot block on the connection.
This keeps transport tests independent of the TUI and lets UI tests substitute a
sender without opening a network connection. The client has no Bubble Tea dependency.

## Why Pi has no equivalent Go package

The existing Pi extension launches revdiff, waits for it to exit, and reads its
annotation output file. That workflow uses revdiff's existing CLI and needs no
Pi-specific transport inside the binary. Amp's split-terminal workflow instead
delivers feedback while both programs remain running, so this implementation
includes a client in revdiff and a receiving plugin in Amp.

Live-refresh behavior remains in `app/ui`, and Git hunk staging remains in
`app/diff`; neither belongs to this transport package.
