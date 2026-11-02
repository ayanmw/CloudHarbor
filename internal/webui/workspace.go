package webui

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
)

func writeWorkspacePage(w http.ResponseWriter, content []byte) {
	// Login assets cannot require the session they are helping establish. Inline
	// the bundled theme for this one public page without widening Gate's routes.
	if bytes.Contains(content, []byte(`id="login-form"`)) {
		tokens, _ := resFiles.ReadFile("res/workspace-tokens.css")
		theme, _ := resFiles.ReadFile("res/workspace-legacy.css")
		style := append([]byte("<style>"), tokens...)
		style = append(style, theme...)
		style = append(style, []byte("</style></head>")...)
		content = bytes.Replace(content, []byte("</head>"), style, 1)
		content = bytes.Replace(content, []byte("<body>"), []byte(`<body class="workspace-legacy workspace-login">`), 1)
		w.Write(content)
		return
	}
	content = bytes.Replace(content, []byte("</head>"), []byte(`<link rel="stylesheet" href="/res/workspace-tokens.css"><link rel="stylesheet" href="/res/workspace-legacy.css"></head>`), 1)
	content = bytes.Replace(content, []byte("</body>"), []byte(`<script src="/res/workspace-legacy.js"></script></body>`), 1)
	w.Write(content)
}

//go:embed app
var workspaceFiles embed.FS

func (s *Server) HandleWorkspace(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	content, err := workspaceFiles.ReadFile("app/index.html")
	if err != nil {
		http.Error(w, "frontend unavailable", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(content)
}
func (s *Server) HandleWorkspaceAssets(w http.ResponseWriter, r *http.Request) {
	files, _ := fs.Sub(workspaceFiles, "app")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.StripPrefix("/app/", http.FileServer(http.FS(files))).ServeHTTP(w, r)
}
