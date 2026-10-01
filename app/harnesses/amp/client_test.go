package amp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientRetryAndAcknowledgement(t *testing.T) {
	var ids []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" || r.URL.Path != "/feedback" {
			t.Error("missing auth or wrong endpoint")
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		ids = append(ids, payload["id"])
		if len(ids) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	root := t.TempDir()
	d := descriptor{Version: 1, URL: server.URL + "/feedback", Token: "secret", Root: root, Thread: "T-test"}
	data, err := json.Marshal(d)
	require.NoError(t, err)
	path := filepath.Join(root, "connection.json")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	c, err := New(path, root)
	require.NoError(t, err)
	require.Error(t, c.Send("first review"))
	require.Error(t, c.Send("different review"))
	require.NoError(t, c.Send("first review"))
	require.NoError(t, c.Send("second review"))
	require.Len(t, ids, 3)
	require.NotEmpty(t, ids[0])
	require.Equal(t, ids[0], ids[1])
	require.NotEqual(t, ids[1], ids[2])
	_, err = New(path, t.TempDir())
	require.ErrorContains(t, err, "different repository")
	for _, url := range []string{"https://example.com/feedback", "http://localhost:1234/feedback", "http://127.0.0.1:1234/other"} {
		d.URL = url
		data, err = json.Marshal(d)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, data, 0o600))
		_, err = New(path, root)
		require.Error(t, err)
	}
}
