package server

import (
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"time"

	"blitiri.com.ar/go/gofer/trace"
)

// handlePut implements PUT for the dir route.
//
// Required diropts: put: { "<prefix>": true } for the request path.
// On success, returns 201 Created (new file) or 204 No Content (replaced),
// with a Last-Modified header reflecting the stored file.
// PUT onto an existing directory returns 409 Conflict.
// Parent directories are created automatically.
// If-Unmodified-Since is honored when the target file already exists.
func handlePut(fs *FileSystem, w http.ResponseWriter, r *http.Request) {
	tr, _ := trace.FromContext(r.Context())

	name := path.Clean(r.URL.Path)
	if !PutEnabled(&fs.opts, name) {
		tr.Printf("PUT not enabled for %q", name)
		http.Error(w, "405 Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if fs.excluded(name) {
		tr.Printf("excluded path %q", name)
		http.Error(w, "404 Not found", http.StatusNotFound)
		return
	}

	abs, err := fs.localPath(name)
	if err != nil {
		tr.Printf("localPath %q: %v", name, err)
		toHTTPError(w, err)
		return
	}

	existed := false
	if fi, err := os.Stat(abs); err == nil {
		if fi.IsDir() {
			tr.Printf("PUT onto directory %q", abs)
			http.Error(w, "409 Conflict (is a directory)",
				http.StatusConflict)
			return
		}
		existed = true
		if !checkIfUnmodifiedSince(tr, r, fi.ModTime()) {
			http.Error(w, "412 Precondition Failed",
				http.StatusPreconditionFailed)
			return
		}
	}

	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		tr.Printf("mkdirall %q: %v", filepath.Dir(abs), err)
		toHTTPError(w, err)
		return
	}

	// Atomic write: temp file in target directory, then rename.
	tmp, err := os.CreateTemp(filepath.Dir(abs), ".gofer-put-*.tmp")
	if err != nil {
		tr.Printf("createtemp: %v", err)
		toHTTPError(w, err)
		return
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			os.Remove(tmpName)
		}
	}()

	if _, err := io.Copy(tmp, r.Body); err != nil {
		tmp.Close()
		tr.Printf("copy body to %q: %v", tmpName, err)
		toHTTPError(w, err)
		return
	}
	if err := tmp.Close(); err != nil {
		tr.Printf("close %q: %v", tmpName, err)
		toHTTPError(w, err)
		return
	}
	if err := os.Rename(tmpName, abs); err != nil {
		tr.Printf("rename %q -> %q: %v", tmpName, abs, err)
		toHTTPError(w, err)
		return
	}
	cleanup = false

	// Return Last-Modified reflecting the stored file, so clients have the
	// validator to use for subsequent If-Unmodified-Since requests without an
	// extra round trip (RFC 9110 §9.3.4). The body is stored untransformed.
	if fi, err := os.Stat(abs); err == nil {
		w.Header().Set("Last-Modified",
			fi.ModTime().UTC().Format(http.TimeFormat))
	}

	if existed {
		tr.Printf("PUT %q: replaced", abs)
		w.WriteHeader(http.StatusNoContent)
	} else {
		tr.Printf("PUT %q: created", abs)
		w.WriteHeader(http.StatusCreated)
	}
}

// handleDelete implements DELETE for the dir route.
//
// Required diropts: delete: { "<prefix>": true } for the request path.
// Files only: DELETE on a directory returns 409 Conflict.
// If-Unmodified-Since is honored.
func handleDelete(fs *FileSystem, w http.ResponseWriter, r *http.Request) {
	tr, _ := trace.FromContext(r.Context())

	name := path.Clean(r.URL.Path)
	if !DeleteEnabled(&fs.opts, name) {
		tr.Printf("DELETE not enabled for %q", name)
		http.Error(w, "405 Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if fs.excluded(name) {
		tr.Printf("excluded path %q", name)
		http.Error(w, "404 Not found", http.StatusNotFound)
		return
	}

	abs, err := fs.localPath(name)
	if err != nil {
		tr.Printf("localPath %q: %v", name, err)
		toHTTPError(w, err)
		return
	}

	fi, err := os.Lstat(abs)
	if err != nil {
		tr.Printf("lstat %q: %v", abs, err)
		toHTTPError(w, err)
		return
	}
	if fi.IsDir() {
		tr.Printf("DELETE on directory %q", abs)
		http.Error(w, "409 Conflict (is a directory)", http.StatusConflict)
		return
	}
	if !checkIfUnmodifiedSince(tr, r, fi.ModTime()) {
		http.Error(w, "412 Precondition Failed",
			http.StatusPreconditionFailed)
		return
	}

	if err := os.Remove(abs); err != nil {
		tr.Printf("remove %q: %v", abs, err)
		toHTTPError(w, err)
		return
	}

	tr.Printf("DELETE %q", abs)
	w.WriteHeader(http.StatusNoContent)
}

// checkIfUnmodifiedSince returns true if the request may proceed: either the
// header is absent/malformed, or the file's mtime is at or before the value
// in the header (compared at 1-second granularity, per RFC 9110 §13.1.4).
func checkIfUnmodifiedSince(tr *trace.Trace, r *http.Request, mtime time.Time) bool {
	h := r.Header.Get("If-Unmodified-Since")
	if h == "" {
		return true
	}
	t, err := http.ParseTime(h)
	if err != nil {
		tr.Printf("if-unmodified-since parse error %q: %v", h, err)
		return true
	}
	tr.Printf("if-unmodified-since: %v, mtime: %v", t, mtime)
	return mtime.Truncate(time.Second).Compare(t) <= 0
}
