package webui

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func New(api http.Handler, webDir string) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/", api)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		if name == "." {
			name = "index.html"
		}
		if !fs.ValidPath(filepath.ToSlash(name)) {
			name = "index.html"
		}
		path := filepath.Join(webDir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			http.ServeFile(w, r, path)
			return
		}
		http.ServeFile(w, r, filepath.Join(webDir, "index.html"))
	})
	return mux
}
