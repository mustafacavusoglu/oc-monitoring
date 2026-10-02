package webui

import (
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// New serves the API, a health probe and the single-page app. Hashed build
// assets are cached forever and served from their build-time .gz sibling when
// the client accepts gzip; index.html is always revalidated.
func New(api http.Handler, webDir string) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/", api)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := filepath.ToSlash(filepath.Clean(strings.TrimPrefix(r.URL.Path, "/")))
		if !fs.ValidPath(name) || !isFile(filepath.Join(webDir, name)) {
			name = "index.html"
		}
		serveFile(w, r, webDir, name)
	})
	return mux
}

func serveFile(w http.ResponseWriter, r *http.Request, webDir, name string) {
	path := filepath.Join(webDir, name)
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	w.Header().Add("Vary", "Accept-Encoding")
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") && isFile(path+".gz") {
		if contentType := mime.TypeByExtension(filepath.Ext(name)); contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.Header().Set("Content-Encoding", "gzip")
		path += ".gz"
	}
	http.ServeFile(w, r, path)
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
