// Package lsp provides a small, read-only client for language-server queries.
package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

type Operation string

const (
	Hover      Operation = "hover"
	Definition Operation = "definition"
	References Operation = "references"
)

type Position struct {
	Path   string
	Line   int
	Column int
}

type Result struct {
	Text      string
	Locations []Position
}

// Server configures one language-server process. LanguageIDs maps file
// extensions (including the leading dot) to LSP language identifiers.
type Server struct {
	Name           string
	Command        string
	Args           []string
	LanguageIDs    map[string]string
	InstallCommand string
}

const maxSourceSize = 16 << 20

var commandContext = exec.CommandContext

type Client struct {
	root     string
	servers  []Server
	mu       sync.Mutex
	closed   bool
	ctx      context.Context
	cancel   context.CancelFunc
	sessions map[int]*session
}

type document struct {
	version int
	text    string
}
type session struct {
	gate chan struct{}
	conn *connection
	docs map[string]document
}

func New(root string, servers ...Server) *Client {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = filepath.Clean(root)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(abs); resolveErr == nil {
		abs = resolved
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{root: abs, servers: append([]Server(nil), servers...), ctx: ctx, cancel: cancel, sessions: make(map[int]*session)}
}

func (c *Client) Query(ctx context.Context, operation Operation, position Position, expectedLine string) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("lsp: nil context")
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(c.ctx, cancel)
	defer stop()
	defer cancel()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if operation != Hover && operation != Definition && operation != References {
		return Result{}, fmt.Errorf("lsp: unsupported operation %q", operation)
	}
	path, serverIndex, languageID, err := c.queryPath(position.Path)
	if err != nil {
		return Result{}, err
	}
	s, err := c.session(serverIndex)
	if err != nil {
		return Result{}, err
	}
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
	source, err := c.readFile(path)
	if err != nil {
		return Result{}, err
	}
	lines := splitLines(source)
	if position.Line < 1 || position.Line > len(lines) {
		return Result{}, fmt.Errorf("lsp: line %d is out of range", position.Line)
	}
	line := lines[position.Line-1]
	if line != expectedLine {
		return Result{}, errors.New("lsp: source changed since diff was loaded")
	}
	if position.Column < 0 || position.Column > len(line) || !utf8.ValidString(line[:position.Column]) {
		return Result{}, errors.New("lsp: column is not a UTF-8 boundary")
	}

	conn, err := c.connection(ctx, serverIndex, s)
	if err != nil {
		return Result{}, err
	}
	if err = c.syncDocuments(ctx, s, conn); err != nil {
		c.dropConnection(s, conn)
		return Result{}, err
	}
	uri := pathURI(path)
	doc := s.docs[uri]
	version := doc.version + 1
	if version == 1 {
		err = conn.notify(ctx, "textDocument/didOpen", map[string]any{"textDocument": map[string]any{
			"uri": uri, "languageId": languageID, "version": version, "text": source,
		}})
	} else if doc.text != source {
		err = conn.notify(ctx, "textDocument/didChange", map[string]any{
			"textDocument":   map[string]any{"uri": uri, "version": version},
			"contentChanges": []any{map[string]any{"text": source}},
		})
	}
	if err != nil {
		c.dropConnection(s, conn)
		return Result{}, err
	}
	if version == 1 || doc.text != source {
		s.docs[uri] = document{version: version, text: source}
	}

	params := map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": position.Line - 1, "character": byteToUTF16(line, position.Column)},
	}
	method := "textDocument/" + string(operation)
	if operation == References {
		params["context"] = map[string]any{"includeDeclaration": true}
	}
	raw, err := conn.request(ctx, method, params)
	if err != nil {
		c.dropConnection(s, conn)
		return Result{}, err
	}
	return c.decodeResult(operation, raw)
}

// InstallCommands supplies explicitly requested installers. Query never installs tools.
func (c *Client) InstallCommands() map[string]string {
	commands := make(map[string]string)
	for _, server := range c.servers {
		if server.Name != "" && server.InstallCommand != "" {
			commands[server.Name] = server.InstallCommand
		}
	}
	return commands
}

func (c *Client) ReadSource(ctx context.Context, path string) (string, error) {
	if ctx == nil {
		return "", errors.New("lsp: nil context")
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	if strings.Contains(path, "://") {
		return "", errors.New("lsp: URI schemes are not supported")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(c.root, path)
	}
	return c.readFile(filepath.Clean(path))
}

func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	sessions := make([]*session, 0, len(c.sessions))
	for _, s := range c.sessions {
		sessions = append(sessions, s)
	}
	c.mu.Unlock()
	c.cancel()
	for _, s := range sessions {
		s.gate <- struct{}{}
		if s.conn != nil {
			conn := s.conn
			ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			timer := time.AfterFunc(250*time.Millisecond, func() { _ = conn.close() })
			_, _ = conn.request(ctx, "shutdown", nil)
			_ = conn.notify(context.Background(), "exit", nil)
			timer.Stop()
			cancel()
			_ = conn.close()
			s.conn = nil
		}
		<-s.gate
	}
	return nil
}

func (c *Client) session(index int) (*session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, errors.New("lsp: client is closed")
	}
	s := c.sessions[index]
	if s == nil {
		s = &session{gate: make(chan struct{}, 1), docs: make(map[string]document)}
		c.sessions[index] = s
	}
	return s, nil
}

func (c *Client) connection(ctx context.Context, index int, s *session) (*connection, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errors.New("lsp: client is closed")
	}
	c.mu.Unlock()
	if s.conn != nil {
		return s.conn, nil
	}
	server := c.servers[index]

	procCtx, procCancel := context.WithCancel(context.Background())
	cmd := commandContext(procCtx, server.Command, server.Args...)
	cmd.Dir = c.root
	stdin, err := cmd.StdinPipe()
	if err != nil {
		procCancel()
		return nil, fmt.Errorf("lsp: %s stdin: %w", server.Command, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		procCancel()
		return nil, fmt.Errorf("lsp: %s stdout: %w", server.Command, err)
	}
	if err = cmd.Start(); err != nil {
		procCancel()
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fmt.Errorf("lsp: %s is not installed; %s", server.Command, server.InstallHint)
		}
		return nil, fmt.Errorf("lsp: start %s: %w", server.Command, err)
	}
	conn := newConnection(server.Command, cmd, stdin, stdout, procCancel)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		_ = conn.close()
		return nil, errors.New("lsp: client is closed")
	}
	s.conn = conn
	s.docs = make(map[string]document)
	c.mu.Unlock()
	initParams := map[string]any{
		"processId": os.Getpid(), "rootUri": pathURI(c.root),
		"workspaceFolders": []any{map[string]any{"uri": pathURI(c.root), "name": filepath.Base(c.root)}},
		"capabilities": map[string]any{
			"general":   map[string]any{"positionEncodings": []string{"utf-16"}},
			"workspace": map[string]any{"applyEdit": false, "workspaceFolders": true},
			"textDocument": map[string]any{
				"synchronization": map[string]any{"dynamicRegistration": false, "didSave": false},
				"hover":           map[string]any{"contentFormat": []string{"markdown", "plaintext"}},
				"definition":      map[string]any{"linkSupport": true},
			},
		},
	}
	var initialized struct {
		Capabilities struct {
			PositionEncoding string `json:"positionEncoding"`
		} `json:"capabilities"`
	}
	raw, err := conn.request(ctx, "initialize", initParams)
	if err == nil {
		err = json.Unmarshal(raw, &initialized)
	}
	if err != nil {
		c.dropConnection(s, conn)
		return nil, fmt.Errorf("lsp: initialize: %w", err)
	}
	if initialized.Capabilities.PositionEncoding != "" && initialized.Capabilities.PositionEncoding != "utf-16" {
		c.dropConnection(s, conn)
		return nil, fmt.Errorf("lsp: unsupported position encoding %q", initialized.Capabilities.PositionEncoding)
	}
	if err = conn.notify(ctx, "initialized", map[string]any{}); err != nil {
		c.dropConnection(s, conn)
		return nil, err
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		_ = conn.close()
		return nil, errors.New("lsp: client is closed")
	}
	c.mu.Unlock()
	return conn, nil
}

func (c *Client) queryPath(path string) (string, int, string, error) {
	if strings.Contains(path, "://") {
		return "", 0, "", errors.New("lsp: queries require a local file")
	}
	ext := strings.ToLower(filepath.Ext(path))
	index, language := -1, ""
	for i, server := range c.servers {
		if id, ok := server.LanguageIDs[ext]; ok {
			index, language = i, id
			break
		}
	}
	if index < 0 {
		return "", 0, "", fmt.Errorf("lsp: unsupported file type %q", ext)
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(c.root, path)
	}
	path = filepath.Clean(path)
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", 0, "", fmt.Errorf("lsp: resolve query path: %w", err)
	}
	path = resolved
	rel, err := filepath.Rel(c.root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", 0, "", errors.New("lsp: query path is outside workspace")
	}
	return path, index, language, nil
}

func (c *Client) dropConnection(s *session, conn *connection) {
	_ = conn.close()
	if s.conn == conn {
		s.conn = nil
		s.docs = make(map[string]document)
	}
}

func (c *Client) syncDocuments(ctx context.Context, s *session, conn *connection) error {
	for uri, doc := range s.docs {
		path, err := uriPath(uri)
		if err != nil {
			return err
		}
		text, err := c.readFile(path)
		if err != nil {
			return err
		}
		if text == doc.text {
			continue
		}
		doc.version++
		if err = conn.notify(ctx, "textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": doc.version}, "contentChanges": []any{map[string]any{"text": text}}}); err != nil {
			return err
		}
		doc.text = text
		s.docs[uri] = doc
	}
	return nil
}

func (c *Client) readFile(path string) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("lsp: read source: %w", err)
	}
	if !st.Mode().IsRegular() {
		return "", errors.New("lsp: source is not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("lsp: read source: %w", err)
	}
	defer f.Close()
	st, err = f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return "", errors.New("lsp: source is not a regular file")
	}
	if st.Size() > maxSourceSize {
		return "", errors.New("lsp: source is too large")
	}
	b, err := io.ReadAll(io.LimitReader(f, maxSourceSize+1))
	if err != nil {
		return "", fmt.Errorf("lsp: read source: %w", err)
	}
	if len(b) > maxSourceSize {
		return "", errors.New("lsp: source is too large")
	}
	if !utf8.Valid(b) || strings.IndexByte(string(b), 0) >= 0 {
		return "", errors.New("lsp: source is not UTF-8 text")
	}
	return string(b), nil
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Split(s, "\n")
}

func byteToUTF16(line string, column int) int {
	n := 0
	for _, r := range line[:column] {
		n += len(utf16.Encode([]rune{r}))
	}
	return n
}

func utf16ToByte(line string, column int) (int, error) {
	units, bytes := 0, 0
	for _, r := range line {
		if units == column {
			return bytes, nil
		}
		width := 1
		if r > 0xffff {
			width = 2
		}
		if units+width > column {
			return 0, errors.New("lsp: position splits UTF-16 surrogate pair")
		}
		units += width
		bytes += utf8.RuneLen(r)
	}
	if units == column {
		return bytes, nil
	}
	return 0, errors.New("lsp: server returned out-of-range position")
}

func pathURI(path string) string {
	p := filepath.ToSlash(path)
	if runtime.GOOS == "windows" && !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

func uriPath(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" || (u.Host != "" && u.Host != "localhost") {
		return "", errors.New("lsp: server returned a non-local URI")
	}
	p := filepath.FromSlash(u.Path)
	if runtime.GOOS == "windows" && len(p) > 2 && p[0] == filepath.Separator && p[2] == ':' {
		p = p[1:]
	}
	return p, nil
}
