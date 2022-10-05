package config

import (
	"log"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"gopkg.in/yaml.v3"
)

func mustURL(r string) *URL {
	u, err := url.Parse(r)
	if err != nil {
		panic(err)
	}
	return (*URL)(u)
}

func TestSimple(t *testing.T) {
	// Note: no TABs in the contents, they are not valid indentation in yaml
	// and the parser complains about "found character that cannot start any
	// token".
	const contents = `
control_addr: "127.0.0.1:9081"

raw:
  ":995":
    certs: "/etc/letsencrypt/live/"
    to: "blerg.com:1995"
    to_tls: true

_routes: &routes
  "/":
    proxy: "http://def/"
  "/dir":
    dir: "/tmp"

http:
  ":http":
    routes:
      <<: *routes
      "/srv":
        proxy: "http://srv/"

https:
  ":https":
    certs: "/etc/letsencrypt/live/"
    routes:
      <<: *routes
      "/":
        proxy: "http://tlsoverrides/"
      "/srv":
        proxy: "http://srv2/"
`

	expected := Config{
		ControlAddr: "127.0.0.1:9081",
		Raw: map[string]Raw{
			":995": {
				Certs: "/etc/letsencrypt/live/",
				To:    "blerg.com:1995",
				ToTLS: true,
			},
		},
		HTTP: map[string]HTTP{
			":http": {
				Routes: map[string]Route{
					"/":    {Proxy: mustURL("http://def/")},
					"/dir": {Dir: "/tmp"},
					"/srv": {Proxy: mustURL("http://srv/")},
				},
			},
		},
		HTTPS: map[string]HTTPS{
			":https": {
				HTTP: HTTP{
					Routes: map[string]Route{
						"/":    {Proxy: mustURL("http://tlsoverrides/")},
						"/dir": {Dir: "/tmp"},
						"/srv": {Proxy: mustURL("http://srv2/")},
					},
				},
				Certs: "/etc/letsencrypt/live/",
			},
		},
	}

	conf, err := LoadString(contents)
	if err != nil {
		log.Fatal(err)
	}

	if diff := cmp.Diff(expected, *conf); diff != "" {
		t.Errorf("configuration is not as expected (-want +got):\n%s", diff)
	}

	conf2, err := LoadString(conf.String())
	if diff := cmp.Diff(expected, *conf2); diff != "" {
		t.Errorf("configuration from string not as expected (-want +got):\n%s", diff)
	}

	// Check that we return an error when we can't access the file.
	conf, err = Load("/doesnotexist")
	if !(conf == nil && strings.Contains(err.Error(), "error reading")) {
		t.Errorf("expected error reading non-existent file, got: %v / %v",
			conf, err)
	}
}

func TestCheck(t *testing.T) {
	// routes must be set.
	contents := `
http:
  ":http":
`
	expectErrs(t, `":http": missing routes`,
		loadAndCheck(t, contents))

	// no actions
	contents = `
http:
  ":http":
    routes:
      "/":
`
	expectErrs(t, `":http": "/": action missing`,
		loadAndCheck(t, contents))

	// too many actions
	contents = `
http:
  ":http":
    routes:
      "/":
        file: "/dev/null"
        dir: "/tmp"
`
	expectErrs(t, `":http": "/": too many actions set`,
		loadAndCheck(t, contents))

	// certs or autocerts must be set.
	contents = `
https:
  ":https":
    routes:
      "/":
        file: "/dev/null"
`
	expectErrs(t, `":https": certs or autocerts must be set`,
		loadAndCheck(t, contents))

	// diropts on a non-directory.
	contents = `
https:
  ":https":
    routes:
      "/":
        file: "/dev/null"
        diropts:
          exclude: ["everything"]
`
	expectErrs(t, `":https": certs or autocerts must be set`,
		loadAndCheck(t, contents))

	// reqlog reference (http).
	contents = `
https:
  ":https":
    certs: "/dev/null"
    routes:
      "/":
        file: "/dev/null"
    reqlog:
      "/": "lalala"
`
	expectErrs(t, `":https": "/": unknown reqlog "lalala"`,
		loadAndCheck(t, contents))

	// reqlog reference (raw).
	contents = `
raw:
  ":1234":
    reqlog: "lalala"
`
	expectErrs(t, `":1234": unknown reqlog "lalala"`,
		loadAndCheck(t, contents))
}

func loadAndCheck(t *testing.T, contents string) []error {
	t.Helper()

	conf, err := LoadString(contents)
	if err != nil {
		t.Errorf("error loading config: %v", err)
	}

	return conf.Check()
}

func expectErrs(t *testing.T, want string, got []error) {
	t.Helper()

	found := false
	for _, e := range got {
		if strings.Contains(e.Error(), want) {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("expected %q, but got %v", want, got)
	}
}

func TestRegexp(t *testing.T) {
	re := Regexp{}
	err := yaml.Unmarshal([]byte(`"ab.d"`), &re)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected := Regexp{
		orig:   "ab.d",
		Regexp: regexp.MustCompile("^(?:ab.d)$"),
	}
	opts := cmp.Comparer(func(x, y Regexp) bool {
		return x.orig == y.orig && x.String() == y.String()
	})
	if diff := cmp.Diff(expected, re, opts); diff != "" {
		t.Errorf("unexpected regexp result (-want +got):\n%s", diff)
	}

	// Error: invalid regexp.
	err = yaml.Unmarshal([]byte(`"*"`), &re)
	if !strings.Contains(err.Error(), "error parsing regexp:") {
		t.Errorf("expected error parsing regexp, got %v", err)
	}
}

func TestURL(t *testing.T) {
	u := URL{}
	err := yaml.Unmarshal([]byte(`"http://a/b/c"`), &u)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected, _ := url.Parse("http://a/b/c")
	if diff := cmp.Diff(URL(*expected), u); diff != "" {
		t.Errorf("unexpected regexp result (-want +got):\n%s", diff)
	}

	// Error: invalid URL.
	err = yaml.Unmarshal([]byte(`":a"`), &u)
	if !strings.Contains(err.Error(), "missing protocol scheme") {
		t.Errorf("expected error parsing url, got %v", err)
	}
}
