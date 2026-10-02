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
without posting feedback. It returns all matching live clients so the UI can offer
a session picker when several match. Malformed or stale registrations are ignored,
not removed. The composition root gates discovery to compatible staged or unstaged
Git working-tree reviews; explicit `--amp` takes precedence. Once selected, clients never switch threads.

`app/revdiff/main.go` wires the client into the UI through three `ModelConfig` fields:

- `Feedback`: the client selected by an explicit `--amp` connection file.
- `DiscoverHarnesses`: the background lookup, retried at one-second intervals
  until a session is found.
- `Harnesses`: named lookups for `:harness connect <type>` and command completion.

Lookups return candidate sessions, not simultaneous connections. The UI connects
to a single match or opens a picker when several match. Only the selected session
is retained. Canceling the picker or running `:harness disconnect` stops automatic
connection until an explicit reconnect, without stopping the plugin session.

Discovery alone never sends feedback. `O` and `:w` request annotation delivery,
waiting for discovery or session selection if needed. `:harness send` (`:hs`)
opens a general-message box. Both kinds of feedback use the same serial sender
and retain unconfirmed content for retry. Unsent drafts pause file refresh, not
session discovery.

The UI depends on its own `FeedbackSender` interface: `Send(content string) error`,
`HarnessName() string`, and `DisplayName() string`. It does not import this package
or interpret Amp-specific identity fields. The name methods use cached metadata,
so rendering never waits for network IO. The footer shows
`Harness (amp): <title> <thread ID>`, plus delivery status. Titles are captured at
plugin registration, and untitled threads show only the ID. Long display text is
shortened from the left.

This boundary lets UI tests substitute a sender without opening a network
connection. Transport tests run independently of the TUI, and the client has no
Bubble Tea dependency.

## Why Pi has no equivalent Go package

The existing Pi extension launches revdiff, waits for it to exit, and reads its
annotation output file. That workflow uses revdiff's existing CLI and needs no
Pi-specific transport inside the binary. Amp's split-terminal workflow instead
delivers feedback while both programs remain running, so this implementation
includes a client in revdiff and a receiving plugin in Amp.

Live-refresh behavior remains in `app/ui`, and Git hunk staging remains in
`app/diff`; neither belongs to this transport package.
