package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"blitiri.com.ar/go/gofer/config"
	"blitiri.com.ar/go/gofer/trace"
)

// put issues a PUT through handlePut and returns the response. ius, if not
// empty, is sent as the If-Unmodified-Since header.
func put(fs *FileSystem, path, body, ius string) *http.Response {
	req := httptest.NewRequest("PUT", "http://unused"+path,
		strings.NewReader(body))
	if ius != "" {
		req.Header.Set("If-Unmodified-Since", ius)
	}
	tr := trace.New("test", path)
	req = req.WithContext(trace.NewContext(req.Context(), tr))
	w := httptest.NewRecorder()
	handlePut(fs, w, req)
	return w.Result()
}

// TestPutLastModified checks that PUT returns a Last-Modified header, and that
// feeding it straight back as If-Unmodified-Since satisfies the precondition
// (rather than failing it). This is what lets a client chain writes without an
// extra round trip to learn the new validator.
func TestPutLastModified(t *testing.T) {
	root := t.TempDir()
	fs := NewFS(root, http.Dir(root), config.DirOpts{
		Put: map[string]bool{"/": true},
	})

	// Create the file; the response carries its Last-Modified.
	resp := put(fs, "/f.txt", "one", "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status = %d, want 201", resp.StatusCode)
	}
	lm := resp.Header.Get("Last-Modified")
	if lm == "" {
		t.Fatal("create: no Last-Modified header")
	}

	// Replacing it with that exact validator must pass the precondition.
	resp = put(fs, "/f.txt", "two", lm)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("replace: status = %d, want 204 (validator did not "+
			"round-trip)", resp.StatusCode)
	}
}
