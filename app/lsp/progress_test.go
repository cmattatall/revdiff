package lsp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProgressTracksIndependentTokens(t *testing.T) {
	var p progressState
	p.setActivity("Loading symbols")
	p.update(json.RawMessage(`{"token":7,"value":{"kind":"begin","title":"Indexing","message":"packages","percentage":0}}`))
	p.update(json.RawMessage(`{"token":"7","value":{"kind":"begin","title":"Dependencies"}}`))
	p.update(json.RawMessage(`{"token":7,"value":{"kind":"report","percentage":43}}`))
	status := p.snapshot("go")
	require.Equal(t, "go", status.Server)
	require.Equal(t, "Dependencies; Indexing: packages (43%)", status.Message)
	require.False(t, status.Since.IsZero())
	p.setActivity("")
	p.update(json.RawMessage(`{"token":"7","value":{"kind":"end"}}`))
	require.Equal(t, "Indexing: packages (43%)", p.snapshot("go").Message)
	p.update(json.RawMessage(`{"token":7,"value":{"kind":"end"}}`))
	require.Empty(t, p.snapshot("go").Message)
	p.update(json.RawMessage(`{"token":7,"value":{"kind":"report","message":"late"}}`))
	require.Empty(t, p.snapshot("go").Message)
}
