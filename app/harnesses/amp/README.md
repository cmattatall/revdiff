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

The composition root in `app/revdiff/main.go` constructs `amp.Client` and injects
it through `ui.ModelConfig.Feedback`. The UI declares the small `FeedbackSender`
interface (`Send(content string) error`); it does not import this package.
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
