// Package amp connects a review to an explicitly selected local Amp thread.
package amp

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type descriptor struct {
	Version int    `json:"version"`
	URL     string `json:"url"`
	Token   string `json:"token"`
	Root    string `json:"root"`
	Thread  string `json:"thread"`
}

// Client sends feedback; retrying identical pending content reuses its request ID.
// Send is called serially by the UI, never concurrently.
type Client struct {
	descriptor descriptor
	http       *http.Client
	pending    string
	id         string
}

// New validates a plugin descriptor against the repository being reviewed.
func New(path, root string) (*Client, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Amp connection: %w", err)
	}
	var d descriptor
	if err = json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("parse Amp connection: %w", err)
	}
	u, err := url.Parse(d.URL)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" ||
		u.User != nil || u.Path != "/feedback" || u.RawQuery != "" || u.Fragment != "" ||
		d.Version != 1 || d.Token == "" || !strings.HasPrefix(d.Thread, "T-") {
		return nil, errors.New("invalid Amp connection descriptor")
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve review root: %w", err)
	}
	connectedRoot, err := filepath.EvalSymlinks(d.Root)
	if err != nil || root != connectedRoot {
		return nil, errors.New("Amp connection belongs to a different repository")
	}
	return &Client{descriptor: d, http: &http.Client{
		Timeout:       15 * time.Second,
		Transport:     &http.Transport{Proxy: nil},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

// Send returns success only after Amp acknowledges appending the feedback.
func (c *Client) Send(content string) error {
	if c.id != "" && c.pending != content {
		return errors.New("retry the pending feedback before sending different content")
	}
	if c.id == "" {
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			return err
		}
		c.id, c.pending = hex.EncodeToString(id[:]), content
	}
	body, err := json.Marshal(struct {
		ID      string `json:"id"`
		Content string `json:"content"`
	}{c.id, content})
	if err != nil {
		return err
	}
	if len(body) > 1024*1024 {
		c.id, c.pending = "", ""
		return errors.New("Amp feedback exceeds 1 MiB; send fewer annotations")
	}
	req, err := http.NewRequest(http.MethodPost, c.descriptor.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.descriptor.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return errors.New("Amp delivery unconfirmed; press O to retry")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusInternalServerError {
		return errors.New("Amp append unconfirmed; check thread, reconnect and relaunch")
	}
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("Amp delivery unconfirmed (HTTP %d); press O to retry", resp.StatusCode)
	}
	c.id, c.pending = "", ""
	return nil
}
