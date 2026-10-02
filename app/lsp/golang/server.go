package golang

import "github.com/umputun/revdiff/app/lsp"

func Server() lsp.Server {
	return lsp.Server{Name: "go", Command: "gopls", LanguageIDs: map[string]string{".go": "go"}, RootMarkers: []string{"go.work", "go.mod"}, InstallCommand: "go install golang.org/x/tools/gopls@latest"}
}
