package lsp

import (
	"encoding/json"
	"fmt"
	"strings"
)

type lspPosition struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}
type lspRange struct {
	Start lspPosition `json:"start"`
	End   lspPosition `json:"end"`
}
type location struct {
	URI   string   `json:"uri"`
	Range lspRange `json:"range"`
}
type locationLink struct {
	TargetURI            string   `json:"targetUri"`
	TargetRange          lspRange `json:"targetRange"`
	TargetSelectionRange lspRange `json:"targetSelectionRange"`
}

func (c *Client) decodeResult(op Operation, raw json.RawMessage) (Result, error) {
	if string(raw) == "null" || len(raw) == 0 {
		return Result{}, nil
	}
	if op == Hover {
		return decodeHover(raw)
	}
	var locs []location
	if raw[0] == '[' {
		var generic []json.RawMessage
		if err := json.Unmarshal(raw, &generic); err != nil {
			return Result{}, fmt.Errorf("lsp: decode locations: %w", err)
		}
		for _, item := range generic {
			var probe struct {
				URI       string `json:"uri"`
				TargetURI string `json:"targetUri"`
			}
			if err := json.Unmarshal(item, &probe); err != nil {
				return Result{}, err
			}
			if probe.TargetURI != "" {
				var link locationLink
				if err := json.Unmarshal(item, &link); err != nil {
					return Result{}, err
				}
				rng := link.TargetSelectionRange
				locs = append(locs, location{URI: link.TargetURI, Range: rng})
			} else {
				var loc location
				if err := json.Unmarshal(item, &loc); err != nil {
					return Result{}, err
				}
				locs = append(locs, loc)
			}
		}
	} else {
		var loc location
		if err := json.Unmarshal(raw, &loc); err != nil {
			return Result{}, err
		}
		locs = append(locs, loc)
	}
	result := Result{Locations: make([]Position, 0, len(locs))}
	sources := make(map[string][]string)
	for _, loc := range locs {
		path, err := uriPath(loc.URI)
		if err != nil {
			return Result{}, err
		}
		lines, ok := sources[path]
		if !ok {
			source, err := c.readFile(path)
			if err != nil {
				return Result{}, err
			}
			lines = splitLines(source)
			sources[path] = lines
		}
		if loc.Range.Start.Line < 0 || loc.Range.Start.Line >= len(lines) {
			return Result{}, fmt.Errorf("lsp: server returned invalid line")
		}
		column, err := utf16ToByte(lines[loc.Range.Start.Line], loc.Range.Start.Character)
		if err != nil {
			return Result{}, err
		}
		result.Locations = append(result.Locations, Position{Path: path, Line: loc.Range.Start.Line + 1, Column: column})
	}
	return result, nil
}

func decodeHover(raw json.RawMessage) (Result, error) {
	var hover struct {
		Contents json.RawMessage `json:"contents"`
	}
	if err := json.Unmarshal(raw, &hover); err != nil {
		return Result{}, fmt.Errorf("lsp: decode hover: %w", err)
	}
	text, err := hoverText(hover.Contents)
	// MarkedString and MarkedString[] are Markdown. Only MarkupContent can
	// explicitly request literal plaintext instead.
	var markup struct {
		Kind string `json:"kind"`
	}
	_ = json.Unmarshal(hover.Contents, &markup)
	return Result{Text: text, Markdown: markup.Kind != "plaintext"}, err
}

func hoverText(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, nil
	}
	var marked struct {
		Language string `json:"language"`
		Value    string `json:"value"`
		Kind     string `json:"kind"`
	}
	if json.Unmarshal(raw, &marked) == nil && (marked.Kind != "" || marked.Language != "" || marked.Value != "") {
		if marked.Language != "" {
			return "```" + marked.Language + "\n" + marked.Value + "\n```", nil
		}
		return marked.Value, nil
	}
	var parts []json.RawMessage
	if err := json.Unmarshal(raw, &parts); err != nil {
		return "", fmt.Errorf("lsp: unsupported hover content")
	}
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		text, err := hoverText(part)
		if err != nil {
			return "", err
		}
		if text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n\n"), nil
}
