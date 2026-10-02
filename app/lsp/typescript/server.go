package typescript

import "github.com/umputun/revdiff/app/lsp"

func Server() lsp.Server {
	return lsp.Server{Name: "typescript", Command: "typescript-language-server", Args: []string{"--stdio"}, LanguageIDs: map[string]string{
		".ts": "typescript", ".mts": "typescript", ".cts": "typescript", ".tsx": "typescriptreact",
		".js": "javascript", ".mjs": "javascript", ".cjs": "javascript", ".jsx": "javascriptreact",
	}, RootMarkers: []string{"package.json", "tsconfig.json", "jsconfig.json"}, InstallCommand: "npm install -g typescript@5 typescript-language-server"}
}
