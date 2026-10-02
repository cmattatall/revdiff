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
)

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
			writeTestMessage(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"capabilities": map[string]any{"positionEncoding": "utf-16"}}})
		case "textDocument/didOpen":
			params := msg["params"].(map[string]any)
			doc := params["textDocument"].(map[string]any)
			if doc["languageId"] != os.Getenv("LSP_EXPECT_LANGUAGE") {
				os.Exit(24)
			}
		case "textDocument/hover", "textDocument/definition":
			if os.Getenv("LSP_HELPER_MODE") == "hang" {
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
				writeTestMessage(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"contents": map[string]any{"kind": "markdown", "value": "write-rejected"}}})
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
