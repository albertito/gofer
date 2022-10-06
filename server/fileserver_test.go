package server

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDirListError(t *testing.T) {
	// Use this file as a "directory" for dirList.
	// We expect it to return a 500 error.
	d := http.Dir(".")
	f, _ := d.Open("fileserver_test.go")
	req := httptest.NewRequest("GET", "http://unused/", nil)
	w := httptest.NewRecorder()
	dirList(w, req, f)
	resp := w.Result()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected internal server error, got %v", resp)
	}
}

func TestToHTTPError(t *testing.T) {
	cases := []struct {
		err       error
		expStatus int
	}{
		{fs.ErrNotExist, http.StatusNotFound},
		{fs.ErrPermission, http.StatusForbidden},
		{fs.ErrInvalid, http.StatusInternalServerError},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		toHTTPError(w, c.err)
		resp := w.Result()
		if resp.StatusCode != c.expStatus {
			t.Errorf("for error %v: expected %v, got %v",
				c.err, c.expStatus, resp.StatusCode)
		}
	}
}

func TestHumanizeInt(t *testing.T) {
	cases := []struct {
		i int64
		e string
	}{
		{10, "10"},
		{1025, "1K"},
		{2 * 1024, "2K"},
		{2 * 1024 * 1024, "2M"},
		{2 * 1024 * 1024 * 1024, "2G"},
	}
	for _, c := range cases {
		s := humanizeInt(c.i)
		if s != c.e {
			t.Errorf("%v: expected %q, got %q", c.i, c.e, s)
		}
	}
}
