package config

import (
	"log"
	"reflect"
	"testing"
)

func TestSimple(t *testing.T) {
	const contents = `
control_addr = "127.0.0.1:9081"

[[raw]]
addr = ":995"
certs = "/etc/letsencrypt/live/"
to = "blerg.com:1995"
to_tls = true

[[http]]
addr = ":http"
base_routes = "default"

  [http.routes]
    "/srv" = "http://srv/"

[[https]]
addr = ":https"
certs = "/etc/letsencrypt/live/"
base_routes = "default"

  [https.routes]
    "/" = "http://tlsoverrides/"
    "/srv" = "http://srv2/"

[routes.default]
"/" = "http://def/"
"/common" = "http://common/"
`

	expected := Config{
		ControlAddr: "127.0.0.1:9081",
		Raw: []Raw{
			Raw{
				Addr:  ":995",
				Certs: "/etc/letsencrypt/live/",
				To:    "blerg.com:1995",
				ToTLS: true,
			},
		},
		HTTP: []*HTTP{
			&HTTP{
				Addr:       ":http",
				BaseRoutes: "default",
				RouteTable: RouteTable{
					"/":       "http://def/",
					"/common": "http://common/",
					"/srv":    "http://srv/",
				},
			},
		},
		HTTPS: []*HTTPS{
			&HTTPS{
				HTTP: HTTP{
					Addr:       ":https",
					BaseRoutes: "default",
					RouteTable: RouteTable{
						"/":       "http://tlsoverrides/",
						"/common": "http://common/",
						"/srv":    "http://srv2/",
					},
				},
				Certs: "/etc/letsencrypt/live/",
			},
		},
		Routes: map[string]RouteTable{
			"default": RouteTable{
				"/":       "http://def/",
				"/common": "http://common/",
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
