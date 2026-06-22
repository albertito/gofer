package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blitiri.com.ar/go/gofer/config"
	"blitiri.com.ar/go/gofer/trace"
)

func TestValidUser(t *testing.T) {
	valid := []string{"alice", "bob@example.com", "a.b", "user-1", "...", "café"}
	for _, u := range valid {
		if err := validUser(u); err != nil {
			t.Errorf("validUser(%q) = %v, want nil", u, err)
		}
	}

	invalid := []string{"", ".", "..", "a/b", `a\b`, "x\x00y", "/etc"}
	for _, u := range invalid {
		if err := validUser(u); err == nil {
			t.Errorf("validUser(%q) = nil, want error", u)
		}
	}
}

func TestLoadAuthFileValidatesUsers(t *testing.T) {
	dir := t.TempDir()

	good := filepath.Join(dir, "good.yaml")
	os.WriteFile(good, []byte("plain:\n  alice: pw\nsha256:\n  bob: abc\n"), 0600)
	if _, err := LoadAuthFile(good); err != nil {
		t.Errorf("LoadAuthFile(good) = %v, want nil", err)
	}

	// An unsafe user name must make the whole file fail to load.
	bad := filepath.Join(dir, "bad.yaml")
	os.WriteFile(bad, []byte("plain:\n  \"../escape\": pw\n"), 0600)
	if _, err := LoadAuthFile(bad); err == nil {
		t.Errorf("LoadAuthFile(bad) = nil, want error")
	}

	// Malformed YAML fails to load.
	malformed := filepath.Join(dir, "malformed.yaml")
	os.WriteFile(malformed, []byte("plain:\n  - a list, not a map\n"), 0600)
	if _, err := LoadAuthFile(malformed); err == nil {
		t.Errorf("LoadAuthFile(malformed) = nil, want error")
	}
}

// serveUser drives an http.Handler with an authenticated user in the request
// context, mimicking what AuthWrapper sets up. An empty user means
// unauthenticated.
func serveUser(h http.Handler, method, target, body, user string) *http.Response {
	var br io.Reader
	if body != "" {
		br = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, br)

	ctx := trace.NewContext(req.Context(), trace.New("test", target))
	if user != "" {
		ctx = withUser(ctx, user)
	}
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w.Result()
}

func TestPerUserDir(t *testing.T) {
	root := t.TempDir()
	opts := config.DirOpts{
		PerUser: true,
		Put:     map[string]bool{"/": true},
		Delete:  map[string]bool{"/": true},
		Listing: map[string]bool{"/": true},
	}
	h := makeDir("/w/", root, opts)

	// PUT as alice creates <root>/alice/f.txt.
	resp := serveUser(h, "PUT", "http://x/w/f.txt", "alice-data", "alice")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("alice PUT: status = %d, want 201", resp.StatusCode)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "alice", "f.txt")); string(got) != "alice-data" {
		t.Errorf("alice file = %q, want %q", got, "alice-data")
	}

	// bob writing the same request path is isolated to his own directory.
	resp = serveUser(h, "PUT", "http://x/w/f.txt", "bob-data", "bob")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("bob PUT: status = %d, want 201", resp.StatusCode)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "bob", "f.txt")); string(got) != "bob-data" {
		t.Errorf("bob file = %q, want %q", got, "bob-data")
	}

	// alice GETs her own file, not bob's.
	resp = serveUser(h, "GET", "http://x/w/f.txt", "", "alice")
	if body, _ := io.ReadAll(resp.Body); string(body) != "alice-data" {
		t.Errorf("alice GET = %q, want %q", body, "alice-data")
	}

	// Unauthenticated request fails closed, writes nothing.
	resp = serveUser(h, "PUT", "http://x/w/anon.txt", "x", "")
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("anon PUT: status = %d, want 403", resp.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(root, "anon.txt")); !os.IsNotExist(err) {
		t.Errorf("anon PUT wrote a file outside any user directory")
	}

	// Traversal stays within the user's own directory: alice cannot reach
	// the sibling top-level "bob" directory. "/w/../bob/x" resolves to
	// <root>/alice/bob/x, not <root>/bob/x.
	resp = serveUser(h, "PUT", "http://x/w/../bob/escape", "nope", "alice")
	if _, err := os.Stat(filepath.Join(root, "alice", "bob", "escape")); err != nil {
		t.Errorf("traversal target not under alice's dir: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "bob", "f.txt")); string(got) != "bob-data" {
		t.Errorf("alice traversal clobbered bob's dir: bob f.txt = %q", got)
	}

	// alice DELETEs her file; bob's remains.
	resp = serveUser(h, "DELETE", "http://x/w/f.txt", "", "alice")
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("alice DELETE: status = %d, want 204", resp.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(root, "alice", "f.txt")); !os.IsNotExist(err) {
		t.Errorf("alice file still exists after DELETE")
	}
	if _, err := os.Stat(filepath.Join(root, "bob", "f.txt")); err != nil {
		t.Errorf("bob file gone after alice DELETE: %v", err)
	}
}
