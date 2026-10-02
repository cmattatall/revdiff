package amp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscover(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	registry := filepath.Join(home, ".cache", "revdiff", "amp")
	client, err := Discover(root)
	require.NoError(t, err)
	require.Nil(t, client)
	require.NoError(t, os.MkdirAll(registry, 0o700))
	register := func(d descriptor) string {
		dir, mkErr := os.MkdirTemp(registry, "session-")
		require.NoError(t, mkErr)
		data, marshalErr := json.Marshal(d)
		require.NoError(t, marshalErr)
		path := filepath.Join(dir, "connection.json")
		require.NoError(t, os.WriteFile(path, data, 0o600))
		return path
	}
	root, err = filepath.EvalSymlinks(root)
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method, "discovery must never send feedback")
		assert.Equal(t, "/feedback", r.URL.Path)
		assert.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
		assert.NoError(t, json.NewEncoder(w).Encode(descriptor{Version: 1, Root: root, Thread: "T-live"}))
	}))
	t.Cleanup(server.Close)
	d := descriptor{Version: 1, URL: server.URL + "/feedback", Root: root, Token: "secret", Thread: "T-live"}
	path := register(d)
	client, err = Discover(root)
	require.NoError(t, err)
	require.Len(t, client, 1)
	require.Equal(t, "T-live", client[0].descriptor.Thread)

	// A sibling, parent, or child directory is not the same Amp workspace.
	child := filepath.Join(root, "child")
	require.NoError(t, os.Mkdir(child, 0o700))
	for _, other := range []string{t.TempDir(), filepath.Dir(root), child} {
		client, err = Discover(other)
		require.NoError(t, err)
		require.Nil(t, client)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	require.NoError(t, os.Symlink(root, alias))
	client, err = Discover(alias)
	require.NoError(t, err)
	require.NotNil(t, client)

	// Ignore a partial file, a dead server, and a reused port with a different thread.
	partial := register(d)
	require.NoError(t, os.WriteFile(partial, []byte("{"), 0o600))
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	stale := d
	stale.URL = dead.URL + "/feedback"
	register(stale)
	stale = d
	stale.Thread = "T-old"
	register(stale)
	client, err = Discover(root)
	require.NoError(t, err)
	require.Len(t, client, 1)
	require.Equal(t, "T-live", client[0].descriptor.Thread)

	// Keep separate connections to the same thread because their retry caches differ.
	extra := register(d)
	client, err = Discover(root)
	require.NoError(t, err)
	require.Len(t, client, 2)
	for _, session := range client {
		require.Equal(t, "T-live", session.DisplayName())
	}
	explicit, err := New(extra, root)
	require.NoError(t, err)
	require.NotNil(t, explicit, "explicit selection still works")
	require.NoError(t, os.Remove(extra))

	second := descriptor{Version: 1, Root: root, Token: "other-secret", Thread: "T-second", Title: "Second review"}
	secondServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "Bearer other-secret", r.Header.Get("Authorization"))
		assert.NoError(t, json.NewEncoder(w).Encode(descriptor{Version: 1, Root: root, Thread: "T-second"}))
	}))
	t.Cleanup(secondServer.Close)
	second.URL = secondServer.URL + "/feedback"
	extra = register(second)
	client, err = Discover(root)
	require.NoError(t, err)
	require.Len(t, client, 2)
	require.ElementsMatch(t, []string{"T-live", "Second review T-second"}, []string{client[0].DisplayName(), client[1].DisplayName()})
	require.NoError(t, os.Remove(extra))

	// Never follow descriptor symlinks or read credentials with public permissions.
	require.NoError(t, os.Symlink(path, extra))
	client, err = Discover(root)
	require.NoError(t, err)
	require.NotNil(t, client)
	require.NoError(t, os.Chmod(path, 0o644))
	client, err = Discover(root)
	require.NoError(t, err)
	require.Nil(t, client)
	require.NoError(t, os.Chmod(registry, 0o755))
	_, err = Discover(root)
	require.ErrorContains(t, err, "private directory")
}

func TestAvailableRejectsUnauthenticatedOrWrongIdentity(t *testing.T) {
	for _, body := range []string{
		`{"version":2,"root":"/review","thread":"T-live"}`,
		`{"version":1,"root":"/other","thread":"T-live"}`,
		`{"version":1,"root":"/review","thread":"T-other"}`,
		`not json`,
	} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, err := w.Write([]byte(body))
				assert.NoError(t, err)
			}))
			defer server.Close()
			c := &Client{descriptor: descriptor{Version: 1, Root: "/review", Thread: "T-live", URL: server.URL}, http: server.Client()}
			require.False(t, c.available())
		})
	}
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(code)
			_, err := w.Write([]byte(`{"version":1,"root":"/review","thread":"T-live"}`))
			assert.NoError(t, err)
		}))
		c := &Client{descriptor: descriptor{Version: 1, Root: "/review", Thread: "T-live", URL: server.URL}, http: server.Client()}
		require.False(t, c.available())
		server.Close()
	}
}
