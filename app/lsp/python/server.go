package python

import "github.com/umputun/revdiff/app/lsp"

func Server() lsp.Server {
	return lsp.Server{Name: "python", Command: "pyright-langserver", Args: []string{"--stdio"}, LanguageIDs: map[string]string{".py": "python", ".pyi": "python"}, RootMarkers: []string{"pyproject.toml", "setup.py", "setup.cfg", "requirements.txt"}, InstallCommand: "npm install -g pyright"}
}
