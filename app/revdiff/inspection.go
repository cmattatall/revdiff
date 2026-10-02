package main

import (
	"context"

	"github.com/umputun/revdiff/app/lsp"
	"github.com/umputun/revdiff/app/lsp/golang"
	"github.com/umputun/revdiff/app/lsp/python"
	"github.com/umputun/revdiff/app/lsp/rust"
	"github.com/umputun/revdiff/app/lsp/typescript"
	"github.com/umputun/revdiff/app/ui"
)

// codeInspector adapts language-server data to the UI's read-only contract.
type codeInspector struct{ *lsp.Client }

func newCodeInspector(opts options, root string) *codeInspector {
	if root == "" || opts.ref() != "" || opts.Stdin || opts.CompareOld != "" || opts.CompareNew != "" {
		return nil
	}
	return &codeInspector{lsp.New(root, golang.Server(), typescript.Server(), python.Server(), rust.Server())}
}

func (c *codeInspector) Query(ctx context.Context, op ui.InspectionOperation, pos ui.InspectionPosition, line string) (ui.InspectionResult, error) {
	result, err := c.Client.Query(ctx, lsp.Operation(op), lsp.Position(pos), line)
	if err != nil {
		return ui.InspectionResult{}, err
	}
	view := ui.InspectionResult{Text: result.Text, Markdown: result.Markdown}
	for _, location := range result.Locations {
		view.Locations = append(view.Locations, ui.InspectionPosition(location))
	}
	return view, nil
}

func (c *codeInspector) Servers() []ui.InspectionServer {
	var servers []ui.InspectionServer
	for _, server := range c.Client.Servers() {
		servers = append(servers, ui.InspectionServer(server))
	}
	return servers
}
