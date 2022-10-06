package reqlog

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
)

func TestBadFormat(t *testing.T) {
	l, err := New("/ignored", 10, "{{bad}}")
	if !(l == nil && strings.Contains(err.Error(), `function "bad" not defined`)) {
		t.Errorf("expected template error, got %v / %v", l, err)
	}
}

func TestStdPaths(t *testing.T) {
	l, err := New("<stdout>", 10, goferFormat)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if l.f != os.Stdout {
		t.Errorf("expected log to os.Stdout, got %v", l.f)
	}

	l, err = New("<stderr>", 10, goferFormat)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if l.f != os.Stderr {
		t.Errorf("expected log to os.Stderr, got %v", l.f)
	}
}

func TestBadFile(t *testing.T) {
	_, err := New("/bad/file", 10, goferFormat)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("expected file does not exists error, got %v", err)
	}
}

func TestQuoteString(t *testing.T) {
	cases := []struct {
		i interface{}
		e string
	}{
		{nil, `""`},
		{"hola", `"hola"`},
		{[]string{"a", "b"}, `"a, b"`},
		{1, "unknown-type-int"},
	}
	for _, c := range cases {
		s := quoteString(c.i)
		if s != c.e {
			t.Errorf("%v: expected %q, got %q", c.i, c.e, s)
		}
	}
}
