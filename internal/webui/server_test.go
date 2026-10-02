package webui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestServesPrecompressedAssetsAndSPAFallback(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.html", "<html></html>")
	write("assets/app-123.js", "plain")
	write("assets/app-123.js.gz", "zipped")
	handler := New(http.NotFoundHandler(), dir)

	get := func(path, encoding string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Accept-Encoding", encoding)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		return recorder
	}

	asset := get("/assets/app-123.js", "gzip, br")
	if asset.Body.String() != "zipped" || asset.Header().Get("Content-Encoding") != "gzip" || asset.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("asset body=%q headers=%v", asset.Body.String(), asset.Header())
	}
	if plain := get("/assets/app-123.js", ""); plain.Body.String() != "plain" {
		t.Fatalf("uncompressed asset body = %q", plain.Body.String())
	}
	if page := get("/batch", "gzip"); page.Body.String() != "<html></html>" || page.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("SPA fallback body=%q headers=%v", page.Body.String(), page.Header())
	}
}
