package config

import (
	"log"
	"net/url"
	"testing"

	"github.com/google/go-cmp/cmp"
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
}
