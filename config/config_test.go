package config

import (
	"log"
	"reflect"
	"testing"
)

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

_proxy: &proxy
  "/": "http://def/"
  "/common": "http://common/"

http:
  ":http":
    proxy:
      <<: *proxy
      "/srv": "http://srv/"

https:
  ":https":
    certs: "/etc/letsencrypt/live/"
    proxy:
      <<: *proxy
      "/": "http://tlsoverrides/"
      "/srv": "http://srv2/"
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
				Proxy: map[string]string{
					"/":       "http://def/",
					"/common": "http://common/",
					"/srv":    "http://srv/",
				},
			},
		},
		HTTPS: map[string]HTTPS{
			":https": {
				HTTP: HTTP{
					Proxy: map[string]string{
						"/":       "http://tlsoverrides/",
						"/common": "http://common/",
						"/srv":    "http://srv2/",
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

	if !reflect.DeepEqual(*conf, expected) {
		t.Errorf("configuration is not as expected")
		t.Errorf("  expected: %v", expected.String())
		t.Errorf("  got:      %v", conf.String())
	}
}
