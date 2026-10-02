package python

import "github.com/umputun/revdiff/app/lsp"

func Server() lsp.Server {
	return lsp.Server{Command: "pyright-langserver", Args: []string{"--stdio"}, LanguageIDs: map[string]string{".py": "python", ".pyi": "python"}, InstallHint: "install with: npm install -g pyright"}
}
