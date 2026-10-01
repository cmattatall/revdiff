package amp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Discover finds a live plugin in exactly root, resolving directory symlinks.
// No match returns nil; multiple matches require explicit --amp selection.
func Discover(root string) (*Client, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("find Amp registry: %w", err)
	}
	registry := filepath.Join(home, ".cache", "revdiff", "amp")
	info, err := os.Lstat(registry)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read Amp registry: %w", err)
	}
	if !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("Amp connection registry must be a private directory")
	}
	entries, err := os.ReadDir(registry)
	if err != nil {
		return nil, fmt.Errorf("list Amp connections: %w", err)
	}
	var selected *Client
	var matches []string
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "session-") {
			continue
		}
		path := filepath.Join(registry, entry.Name(), "connection.json")
		file, statErr := os.Lstat(path)
		if statErr != nil || !file.Mode().IsRegular() || file.Mode().Perm()&0o077 != 0 {
			continue
		}
		client, readErr := New(path, root)
		if readErr != nil {
			continue // Partial, stale, or another directory's registration.
		}
		live := client.available()
		client.http.CloseIdleConnections()
		if live {
			selected = client
			matches = append(matches, fmt.Sprintf("  %s: --amp %q", client.descriptor.Thread, path))
		}
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("multiple Amp sessions in this directory; disconnect extras or select one explicitly:\n%s", strings.Join(matches, "\n"))
	}
	return selected, nil
}

// available checks identity as well as liveness, without appending a message.
func (c *Client) available() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.descriptor.URL, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+c.descriptor.Token)
	resp, err := c.http.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var identity descriptor
	return resp.StatusCode == http.StatusOK &&
		json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&identity) == nil &&
		identity.Version == c.descriptor.Version && identity.Root == c.descriptor.Root && identity.Thread == c.descriptor.Thread
}
