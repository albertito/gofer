package server

import "testing"

func TestPrefixMatch(t *testing.T) {
	m := map[string]bool{
		"/":          true,
		"/readonly/": false,
		"/deep/sub/": true,
	}
	cases := []struct {
		name string
		want bool
	}{
		// Root prefix.
		{"/", true},
		{"/anything", true},
		{"/anything/else", true},

		// Longest prefix wins.
		{"/readonly/x", false},
		// filepath.Clean strips the trailing slash from both name and
		// prefix, so "/readonly" matches the "/readonly/" entry too.
		{"/readonly", false},
		{"/deep/sub/x", true},

		// Empty name treated as "/".
		{"", true},
	}
	for _, c := range cases {
		got := prefixMatch(m, c.name)
		if got != c.want {
			t.Errorf("prefixMatch(%q) = %v, want %v", c.name, got, c.want)
		}
	}

	// Empty map -> always false.
	if prefixMatch(nil, "/anything") {
		t.Errorf("prefixMatch(nil, ...) = true, want false")
	}
}
