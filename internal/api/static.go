package api

import (
	"io/fs"
	"net/http"
	"strings"
)

// spaHandler serves static files from an embedded filesystem.
// For paths not found, it returns index.html to support SPA routing.
type spaHandler struct {
	staticFS http.Handler
	indexFS  fs.FS
}

func newSPAHandler(webFS fs.FS) (*spaHandler, error) {
	return &spaHandler{
		staticFS: http.FileServer(http.FS(webFS)),
		indexFS:  webFS,
	}, nil
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Try to serve the requested file
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}

	// Check if file exists
	f, err := h.indexFS.Open(path)
	if err == nil {
		f.Close()
		h.staticFS.ServeHTTP(w, r)
		return
	}

	// File not found - serve index.html for SPA routing
	indexContent, err := fs.ReadFile(h.indexFS, "index.html")
	if err != nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(indexContent)
}
