package typescript

import "github.com/umputun/revdiff/app/lsp"

func Server() lsp.Server {
	return lsp.Server{Command: "typescript-language-server", Args: []string{"--stdio"}, LanguageIDs: map[string]string{
		".ts": "typescript", ".mts": "typescript", ".cts": "typescript", ".tsx": "typescriptreact",
		".js": "javascript", ".mjs": "javascript", ".cjs": "javascript", ".jsx": "javascriptreact",
	}, InstallHint: "install with: npm install -g typescript typescript-language-server"}
}
