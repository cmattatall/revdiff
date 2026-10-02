package lsp_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/umputun/revdiff/app/lsp"
	"github.com/umputun/revdiff/app/lsp/golang"
	"github.com/umputun/revdiff/app/lsp/python"
	"github.com/umputun/revdiff/app/lsp/rust"
	"github.com/umputun/revdiff/app/lsp/typescript"
)

func TestSymbolsUseLanguageTokens(t *testing.T) {
	for _, tc := range []struct {
		path, source string
		line         int
		want         []lsp.Symbol
	}{
		{"main.go", "package p\ntype Thing struct { Name string } // notASymbol", 2, []lsp.Symbol{{"Thing", 5}, {"Name", 20}, {"string", 25}}},
		{"main.go", "package p\nfunc f() { π := π + π }", 2, []lsp.Symbol{{"f", 5}, {"π", 11}, {"π", 17}, {"π", 22}}},
		{"main.go", "package p\n/*\nvar imaginary string\n*/\nvar real string", 3, nil},
		{"main.go", "package p\nvar raw = `\nstruct inside string\n`", 3, nil},
		{"main.ts", "const name: string = 'words here'; // comment", 1, []lsp.Symbol{{"name", 6}, {"string", 12}}},
		{"main.py", "def greet(name: str):\n    return name # comment", 1, []lsp.Symbol{{"greet", 4}, {"name", 10}, {"str", 16}}},
		{"main.rs", "struct Person { name: String } // comment", 1, []lsp.Symbol{{"Person", 7}, {"name", 16}, {"String", 22}}},
	} {
		t.Run(tc.path+"/"+tc.source, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, tc.path), []byte(tc.source), 0o600))
			client := lsp.New(root, golang.Server(), typescript.Server(), python.Server(), rust.Server())
			defer client.Close()
			line := strings.Split(tc.source, "\n")[tc.line-1]
			got, err := client.Symbols(context.Background(), lsp.Position{Path: tc.path, Line: tc.line}, line)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
			_, err = client.Symbols(context.Background(), lsp.Position{Path: tc.path, Line: tc.line}, "stale")
			require.ErrorContains(t, err, "source changed")
		})
	}
}
