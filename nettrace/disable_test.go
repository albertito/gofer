package nettrace

import (
	"errors"
	"os"
	"testing"
)

func TestDisabledTrace(t *testing.T) {
	// Disable all tracing functions for the duration of this test.
	// Re-enable them afterwards.
	Disable = true
	defer func() { Disable = false }()

	// Check that New(), when Disable is true, returns a disabledTrace and
	// doesn't update the intenral structures.
	tr := New("family", "title")
	if _, ok := tr.(disabledTrace); !ok {
		t.Errorf("New() = %T, want disabledTrace", tr)
	}

	if ft, ok := families["family"]; ok {
		t.Errorf("families['family'] = %v, expected it to be empty", ft)
	}

	// Call the methods of disabledTrace to ensure they don't panic or, when
	// they have potential side-effects that we can check, that they don't.
	ctr := tr.NewChild("family-2", "title")
	if _, ok := ctr.(disabledTrace); !ok {
		t.Errorf("NewChild() = %T, want disabledTrace", ctr)
	}
	tr.Link(ctr, "msg")
	tr.SetMaxEvents(10)
	tr.SetError()
	tr.Printf("hola")

	err := tr.Errorf("this is an error: %w", os.ErrNotExist)
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("error = %v, want wrapped os.ErrNotExist", err)
	}

	tr.Print("this is a string")

	err = tr.Error(os.ErrNotExist)
	if err != os.ErrNotExist {
		t.Errorf("error = %v, want os.ErrNotExist", err)
	}

	tr.Finish()
}
