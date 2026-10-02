package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHoverContentFormat(t *testing.T) {
	for _, tc := range []struct {
		contents string
		text     string
		markdown bool
	}{
		{`{"kind":"markdown","value":"` + "```go\\nvar x int\\n```" + `"}`, "```go\nvar x int\n```", true},
		{`{"kind":"plaintext","value":"` + "```go\\nvar x int\\n```" + `"}`, "```go\nvar x int\n```", false},
		{`[{"language":"python","value":"def f(): pass"},"Docs"]`, "```python\ndef f(): pass\n```\n\nDocs", true},
	} {
		result, err := decodeHover(json.RawMessage(`{"contents":` + tc.contents + `}`))
		require.NoError(t, err)
		require.Equal(t, tc.text, result.Text)
		require.Equal(t, tc.markdown, result.Markdown)
	}
}

func TestDocumentSymbols(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	source := "😀Parent\n Child"
	require.NoError(t, os.WriteFile(path, []byte(source), 0o600))
	path, err := filepath.EvalSymlinks(path)
	require.NoError(t, err)
	useFakeServer(t, "normal")
	c := New(root, Server{Name: "test", Command: "test-server", LanguageIDs: map[string]string{".go": "go"}})
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := c.Query(ctx, DocumentSymbols, Position{Path: path}, "")
	require.NoError(t, err)
	require.Equal(t, []DocumentSymbol{{"Parent", Position{path, 1, 4}}, {"Parent.Child", Position{path, 2, 1}}}, result.Symbols)
	flat, err := json.Marshal([]any{map[string]any{"name": "Child", "containerName": "Parent", "location": map[string]any{"uri": pathURI(path), "range": testRange(1, 1)}}})
	require.NoError(t, err)
	result, err = c.decodeDocumentSymbols(flat, path, source)
	require.NoError(t, err)
	require.Equal(t, []DocumentSymbol{{"Parent.Child", Position{path, 2, 1}}}, result.Symbols)
	result, err = c.decodeDocumentSymbols(json.RawMessage("null"), path, source)
	require.NoError(t, err)
	require.Empty(t, result.Symbols)
	_, err = c.decodeDocumentSymbols(json.RawMessage(`[{"name":"bad"}]`), path, source)
	require.ErrorContains(t, err, "invalid document symbol location")
}

func TestClientLifecycleUnicodeAndReadOnly(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	line := "😀foo()"
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	useFakeServer(t, "normal")
	c := New(root, Server{Command: "fake", LanguageIDs: map[string]string{".go": "go"}})
	defer c.Close()

	hover, err := c.Query(context.Background(), Hover, Position{Path: path, Line: 1, Column: 4}, line)
	if err != nil {
		t.Fatal(err)
	}
	if hover.Text != "write-rejected" {
		t.Fatalf("unexpected hover: %#v", hover)
	}
	def, err := c.Query(context.Background(), Definition, Position{Path: path, Line: 1, Column: 4}, line)
	if err != nil {
		t.Fatal(err)
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	want := Position{Path: resolvedPath, Line: 1, Column: 4}
	if len(def.Locations) != 1 || def.Locations[0] != want {
		t.Fatalf("locations = %#v, want %#v", def.Locations, want)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestStaleAndSourceValidation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := New(root, Server{Command: "fake", LanguageIDs: map[string]string{".go": "go"}})
	if _, err := c.Query(context.Background(), Hover, Position{Path: path, Line: 1}, "package old"); err == nil {
		t.Fatal("expected stale source error")
	}
	if _, err := c.Query(context.Background(), Hover, Position{Path: "../other.go", Line: 1}, ""); err == nil {
		t.Fatal("expected outside-root error")
	}
	if _, err := c.ReadSource(context.Background(), "https://example.test/x.go"); err == nil {
		t.Fatal("expected URI rejection")
	}
	if got, err := c.ReadSource(context.Background(), path); err != nil || got != "package main\n" {
		t.Fatalf("ReadSource = %q, %v", got, err)
	}
}

func TestCancellationAndCloseDoNotBlock(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	useFakeServer(t, "hang")
	c := New(root, Server{Command: "fake", LanguageIDs: map[string]string{".go": "go"}})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := c.Query(ctx, Hover, Position{Path: path, Line: 1}, "package main"); err == nil {
		t.Fatal("expected cancellation")
	}
	done := make(chan struct{})
	go func() { _ = c.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close blocked")
	}
}

func TestLanguageSelectionAndIndependentSessions(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"main.go", "main.ts"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("😀x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := commandContext
	var mu sync.Mutex
	starts := map[string]int{}
	commandContext = func(ctx context.Context, name string, _ ...string) *exec.Cmd {
		mu.Lock()
		starts[name]++
		mu.Unlock()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestLSPHelperProcess", "--")
		language := map[string]string{"go-server": "go", "ts-server": "typescript"}[name]
		cmd.Env = append(os.Environ(), "GO_WANT_LSP_HELPER=1", "LSP_HELPER_MODE=normal", "LSP_EXPECT_LANGUAGE="+language)
		return cmd
	}
	t.Cleanup(func() { commandContext = old })
	c := New(root,
		Server{Command: "go-server", LanguageIDs: map[string]string{".go": "go"}},
		Server{Command: "ts-server", LanguageIDs: map[string]string{".ts": "typescript", ".js": "javascript"}},
	)
	defer c.Close()
	for _, name := range []string{"main.go", "main.ts"} {
		if _, err := c.Query(context.Background(), Hover, Position{Path: name, Line: 1, Column: 4}, "😀x"); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if starts["go-server"] != 1 || starts["ts-server"] != 1 {
		t.Fatalf("server starts = %#v", starts)
	}
	if _, err := c.Query(context.Background(), Hover, Position{Path: "main.txt", Line: 1}, ""); err == nil || !strings.Contains(err.Error(), "unsupported file type") {
		t.Fatalf("unsupported error = %v", err)
	}
}

func TestCloseCancelsActiveQuery(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	require.NoError(t, os.WriteFile(path, []byte("package main\n"), 0o600))
	marker := filepath.Join(root, "query-started")
	t.Setenv("LSP_READY_FILE", marker)
	useFakeServer(t, "hang")
	c := New(root, Server{Command: "fake", LanguageIDs: map[string]string{".go": "go"}})
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := c.Query(ctx, Hover, Position{Path: path, Line: 1}, "package main")
		result <- err
	}()
	require.Eventually(t, func() bool { _, err := os.Stat(marker); return err == nil }, 3*time.Second, 10*time.Millisecond)
	closed := make(chan struct{})
	go func() { _ = c.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close must cancel active requests without waiting for their deadline")
	}
	require.Error(t, <-result)
}

func TestQueuedQueryCancellation(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600))
	c := New(root, Server{Command: "fake", LanguageIDs: map[string]string{".go": "go"}})
	defer c.Close()
	s, err := c.session(0, c.root)
	require.NoError(t, err)
	s.gate <- struct{}{}
	defer func() { <-s.gate }()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = c.Query(ctx, Hover, Position{Path: "main.go", Line: 1}, "package main")
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestWorkspaceRootsAndReuse(t *testing.T) {
	root := t.TempDir()
	useFakeServer(t, "root")
	c := New(root, Server{Command: "fake", LanguageIDs: map[string]string{".go": "go"}, RootMarkers: []string{"go.work", "go.mod"}})
	defer c.Close()
	for _, project := range []string{"one", "two"} {
		dir := filepath.Join(c.root, project)
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "nested"), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), nil, 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "nested", "go.mod"), nil, 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "nested", "main.go"), []byte("😀symbol\n"), 0o600))
	}
	for _, project := range []string{"one", "two", "one"} {
		result, err := c.Query(context.Background(), Hover, Position{Path: project + "/nested/main.go", Line: 1, Column: 4}, "😀symbol")
		require.NoError(t, err)
		require.Equal(t, filepath.Join(c.root, project), result.Text, "the outermost marker inside the review root wins")
	}
	require.Len(t, c.sessions, 2, "independent roots have separate sessions, repeated queries reuse them")
	require.NoError(t, os.WriteFile(filepath.Join(c.root, "go.work"), nil, 0o600))
	result, err := c.Query(context.Background(), Hover, Position{Path: "one/nested/main.go", Line: 1, Column: 4}, "😀symbol")
	require.NoError(t, err)
	require.Equal(t, c.root, result.Text, "a review-root workspace manifest covers nested projects")
}

func TestServerDiscoveryAndInstallCommands(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "example-server")
	require.NoError(t, os.WriteFile(executable, []byte("#!/bin/sh\nexit 1\n"), 0o700))
	t.Setenv("PATH", root)
	c := New(root,
		Server{Name: "example", Command: "example-server", InstallCommand: "install example"},
		Server{Name: "other", Command: "other-server", InstallCommand: "install other"},
	)
	defer c.Close()
	require.Equal(t, []ServerStatus{{Name: "example", Command: "example-server", Path: executable}, {Name: "other", Command: "other-server"}}, c.Servers())
	require.Equal(t, map[string]string{"example": "install example", "other": "install other"}, c.InstallCommands())
	require.Empty(t, c.sessions, "discovery must not start a server")
}

func useFakeServer(t *testing.T, mode string) {
	t.Helper()
	old := commandContext
	commandContext = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestLSPHelperProcess", "--")
		cmd.Env = append(os.Environ(), "GO_WANT_LSP_HELPER=1", "LSP_HELPER_MODE="+mode, "LSP_EXPECT_LANGUAGE=go")
		return cmd
	}
	t.Cleanup(func() { commandContext = old })
}

func TestLSPHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_LSP_HELPER") != "1" {
		return
	}
	r := bufio.NewReader(os.Stdin)
	for {
		msg, err := readTestMessage(r)
		if err != nil {
			os.Exit(0)
		}
		method, _ := msg["method"].(string)
		id, hasID := msg["id"]
		switch method {
		case "initialize":
			cwd, _ := os.Getwd()
			if msg["params"].(map[string]any)["rootUri"] != pathURI(cwd) {
				os.Exit(25)
			}
			writeTestMessage(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"capabilities": map[string]any{"positionEncoding": "utf-16"}}})
		case "textDocument/didOpen":
			params := msg["params"].(map[string]any)
			doc := params["textDocument"].(map[string]any)
			if doc["languageId"] != os.Getenv("LSP_EXPECT_LANGUAGE") {
				os.Exit(24)
			}
		case "textDocument/documentSymbol":
			params := msg["params"].(map[string]any)
			if _, ok := params["position"]; ok {
				os.Exit(26)
			}
			writeTestMessage(map[string]any{"jsonrpc": "2.0", "id": id, "result": []any{map[string]any{
				"name": "Parent", "range": testRange(0, 0), "selectionRange": testRange(0, 2),
				"children": []any{map[string]any{"name": "Child", "range": testRange(1, 0), "selectionRange": testRange(1, 1)}},
			}}})
		case "textDocument/hover", "textDocument/definition":
			if os.Getenv("LSP_HELPER_MODE") == "hang" {
				if marker := os.Getenv("LSP_READY_FILE"); marker != "" {
					_ = os.WriteFile(marker, nil, 0o600)
				}
				continue
			}
			params := msg["params"].(map[string]any)
			pos := params["position"].(map[string]any)
			if pos["character"].(float64) != 2 {
				os.Exit(21)
			}
			writeTestMessage(map[string]any{"jsonrpc": "2.0", "id": 99, "method": "workspace/applyEdit", "params": map[string]any{"edit": map[string]any{}}})
			for {
				answer, err := readTestMessage(r)
				if err != nil {
					os.Exit(22)
				}
				if answer["id"] == float64(99) {
					result := answer["result"].(map[string]any)
					if applied, _ := result["applied"].(bool); applied {
						os.Exit(23)
					}
					break
				}
			}
			if method == "textDocument/hover" {
				text := "write-rejected"
				if os.Getenv("LSP_HELPER_MODE") == "root" {
					text, _ = os.Getwd()
				}
				writeTestMessage(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"contents": map[string]any{"kind": "markdown", "value": text}}})
			} else {
				uri := params["textDocument"].(map[string]any)["uri"]
				writeTestMessage(map[string]any{"jsonrpc": "2.0", "id": id, "result": []any{map[string]any{"targetUri": uri, "targetRange": testRange(0, 2), "targetSelectionRange": testRange(0, 2)}}})
			}
		case "shutdown":
			if hasID {
				writeTestMessage(map[string]any{"jsonrpc": "2.0", "id": id, "result": nil})
			}
		case "exit":
			os.Exit(0)
		}
	}
}

func testRange(line, character int) map[string]any {
	p := map[string]any{"line": line, "character": character}
	return map[string]any{"start": p, "end": p}
}

func readTestMessage(r *bufio.Reader) (map[string]any, error) {
	length := 0
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if value, ok := strings.CutPrefix(strings.ToLower(line), "content-length:"); ok {
			length, _ = strconv.Atoi(strings.TrimSpace(value))
		}
	}
	b := make([]byte, length)
	if _, err := io.ReadFull(r, b); err != nil {
		return nil, err
	}
	var msg map[string]any
	return msg, json.Unmarshal(b, &msg)
}

func writeTestMessage(msg any) {
	b, _ := json.Marshal(msg)
	_, _ = fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n", len(b))
	_, _ = os.Stdout.Write(b)
}
