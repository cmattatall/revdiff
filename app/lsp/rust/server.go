package rust

import "github.com/umputun/revdiff/app/lsp"

func Server() lsp.Server {
	return lsp.Server{Name: "rust", Command: "rust-analyzer", LanguageIDs: map[string]string{".rs": "rust"}, RootMarkers: []string{"Cargo.toml", "Cargo.lock"}, InstallCommand: "rustup component add rust-analyzer"}
}
