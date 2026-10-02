package golang

import "github.com/umputun/revdiff/app/lsp"

func Server() lsp.Server {
	return lsp.Server{Command: "gopls", LanguageIDs: map[string]string{".go": "go"}, InstallHint: "install with: go install golang.org/x/tools/gopls@latest"}
}
