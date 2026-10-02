package rust

import "github.com/umputun/revdiff/app/lsp"

func Server() lsp.Server {
	return lsp.Server{Command: "rust-analyzer", LanguageIDs: map[string]string{".rs": "rust"}, InstallHint: "install rust-analyzer from https://rust-analyzer.github.io/"}
}
