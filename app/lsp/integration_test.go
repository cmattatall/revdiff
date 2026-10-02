package lsp_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/umputun/revdiff/app/lsp"
	"github.com/umputun/revdiff/app/lsp/golang"
	"github.com/umputun/revdiff/app/lsp/python"
	"github.com/umputun/revdiff/app/lsp/rust"
	"github.com/umputun/revdiff/app/lsp/typescript"
)

// Opt in because real servers are optional tools, not build dependencies.
func TestInstalledServers(t *testing.T) {
	if os.Getenv("REVDIFF_LSP_INTEGRATION") != "1" {
		t.Skip("set REVDIFF_LSP_INTEGRATION=1 to exercise installed language servers")
	}
	for _, tc := range []struct {
		name, path, source string
		files              map[string]string
		server             lsp.Server
		definition, use    int
	}{
		{"go", "main.go", "package main\nfunc twice(n int) int { return n * 2 }\nfunc main() { _ = twice(21) }\n", map[string]string{"go.mod": "module example.test/inspect\n\ngo 1.24\n"}, golang.Server(), 2, 3},
		{"typescript", "main.ts", "function twice(n: number): number { return n * 2; }\nconst answer = twice(21);\n", map[string]string{"tsconfig.json": "{}"}, typescript.Server(), 1, 2},
		{"python", "main.py", "def twice(n: int) -> int:\n    return n * 2\nanswer = twice(21)\n", nil, python.Server(), 1, 3},
		{"rust", "src/main.rs", "fn twice(n: i32) -> i32 { n * 2 }\nfn main() { let _answer = twice(21); }\n", map[string]string{"Cargo.toml": "[package]\nname = \"inspect\"\nversion = \"0.1.0\"\nedition = \"2021\"\n"}, rust.Server(), 1, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := exec.LookPath(tc.server.Command); err != nil {
				t.Skip(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			root := t.TempDir()
			if tc.name == "typescript" {
				compiler, err := exec.LookPath("tsc")
				require.NoError(t, err)
				compiler, err = filepath.EvalSymlinks(compiler)
				require.NoError(t, err)
				require.NoError(t, os.Mkdir(filepath.Join(root, "node_modules"), 0o700))
				require.NoError(t, os.Symlink(filepath.Dir(filepath.Dir(compiler)), filepath.Join(root, "node_modules/typescript")))
			}
			require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, tc.path)), 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(root, tc.path), []byte(tc.source), 0o600))
			for path, text := range tc.files {
				require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(text), 0o600))
			}
			client := lsp.New(root, tc.server)
			defer client.Close()
			lines := strings.Split(tc.source, "\n")
			line := lines[tc.use-1]
			position := lsp.Position{Path: tc.path, Line: tc.use, Column: strings.Index(line, "twice")}
			hover, err := client.Query(ctx, lsp.Hover, position, line)
			require.NoError(t, err)
			require.Contains(t, hover.Text, "twice")
			definition, err := client.Query(ctx, lsp.Definition, position, line)
			require.NoError(t, err)
			require.Len(t, definition.Locations, 1)
			require.Equal(t, tc.definition, definition.Locations[0].Line)
			require.Equal(t, strings.Index(lines[tc.definition-1], "twice"), definition.Locations[0].Column)
			source, err := client.ReadSource(ctx, definition.Locations[0].Path)
			require.NoError(t, err)
			require.Equal(t, tc.source, source)
			references, err := client.Query(ctx, lsp.References, position, line)
			require.NoError(t, err)
			var foundUse bool
			for _, reference := range references.Locations {
				if reference.Line == tc.use && reference.Column == position.Column {
					foundUse = true
				}
			}
			require.True(t, foundUse, "references must include the call site: %+v", references)
			symbols, err := client.Query(ctx, lsp.DocumentSymbols, lsp.Position{Path: tc.path}, "")
			require.NoError(t, err)
			var foundDefinition bool
			for _, symbol := range symbols.Symbols {
				if strings.Contains(symbol.Name, "twice") && symbol.Position.Line == tc.definition {
					foundDefinition = true
				}
			}
			require.True(t, foundDefinition, "document symbols must include the function declaration: %+v", symbols)
		})
	}
}
