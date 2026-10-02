package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// Symbol is a lexical name that can be inspected, with a zero-based byte column.
type Symbol struct {
	Name   string
	Column int
}

type DocumentSymbol struct {
	Name     string
	Position Position
}

type documentSymbol struct {
	Name           string           `json:"name"`
	ContainerName  string           `json:"containerName"`
	SelectionRange *lspRange        `json:"selectionRange"`
	Location       *location        `json:"location"`
	Children       []documentSymbol `json:"children"`
}

func (c *Client) decodeDocumentSymbols(raw json.RawMessage, path, source string) (Result, error) {
	var symbols []documentSymbol
	if err := json.Unmarshal(raw, &symbols); err != nil {
		return Result{}, fmt.Errorf("lsp: decode document symbols: %w", err)
	}
	result := Result{}
	err := c.appendDocumentSymbols(&result, symbols, "", path, splitLines(source))
	return result, err
}

func (c *Client) appendDocumentSymbols(result *Result, symbols []documentSymbol, parent, path string, lines []string) error {
	for _, symbol := range symbols {
		rng := symbol.SelectionRange
		if symbol.Location != nil {
			target, err := uriPath(symbol.Location.URI)
			if err != nil {
				return err
			}
			if filepath.Clean(target) != filepath.Clean(path) {
				continue
			}
			rng = &symbol.Location.Range
		}
		if rng == nil || rng.Start.Line < 0 || rng.Start.Line >= len(lines) {
			return errors.New("lsp: invalid document symbol location")
		}
		column, err := utf16ToByte(lines[rng.Start.Line], rng.Start.Character)
		if err != nil {
			return err
		}
		container := parent
		if container == "" {
			container = symbol.ContainerName
		}
		name := symbol.Name
		if container != "" {
			name = container + "." + name
		}
		result.Symbols = append(result.Symbols, DocumentSymbol{Name: name, Position: Position{Path: path, Line: rng.Start.Line + 1, Column: column}})
		if err := c.appendDocumentSymbols(result, symbol.Children, name, path, lines); err != nil {
			return err
		}
	}
	return nil
}

// Symbols uses the whole source so multiline strings and comments retain their
// language context. It does not start a language server or claim semantic validity.
func (c *Client) Symbols(ctx context.Context, pos Position, expectedLine string) ([]Symbol, error) {
	path, _, language, err := c.queryPath(pos.Path)
	if err != nil {
		return nil, err
	}
	source, err := c.ReadSource(ctx, path)
	if err != nil {
		return nil, err
	}
	lines := splitLines(source)
	if pos.Line < 1 || pos.Line > len(lines) || lines[pos.Line-1] != expectedLine {
		return nil, fmt.Errorf("lsp: source changed since diff was loaded")
	}
	lexer := lexers.Get(language)
	if lexer == nil {
		lexer = lexers.Match(path)
	}
	if lexer == nil {
		return nil, fmt.Errorf("lsp: no symbol lexer for %s", language)
	}
	iter, err := lexer.Tokenise(nil, source)
	if err != nil {
		return nil, fmt.Errorf("lsp: tokenize source: %w", err)
	}
	var symbols []Symbol
	line, column := 1, 0
	for token := iter(); token != chroma.EOF && line <= pos.Line; token = iter() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if line == pos.Line && (token.Type.InCategory(chroma.Name) || token.Type == chroma.KeywordType || token.Type == chroma.KeywordConstant) {
			symbols = append(symbols, Symbol{Name: token.Value, Column: column})
		}
		if n := strings.Count(token.Value, "\n"); n > 0 {
			line += n
			column = len(token.Value) - strings.LastIndexByte(token.Value, '\n') - 1
		} else {
			column += len(token.Value)
		}
	}
	return symbols, nil
}
