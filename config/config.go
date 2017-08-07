// Package config implements the gofer configuration.
package config

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	ControlAddr string `toml:"control_addr"`

	HTTP  []*HTTP
	HTTPS []*HTTPS
	Raw   []Raw

	// Map of name -> routes for HTTP(S).
	Routes map[string]RouteTable

	// Undecoded fields - private so we don't serialize them.
	undecoded []string
}

type HTTP struct {
	Addr       string
	RouteTable RouteTable `toml:"routes",omitempty`
	BaseRoutes string     `toml:"base_routes"`
}

type HTTPS struct {
	HTTP
	Certs string
}

type Raw struct {
	Addr  string
	Certs string
	To    string
	ToTLS bool `toml:"to_tls",omitempty`
}

type RouteTable map[string]string

// mergeRoutes merges the table src into dst, by adding the entries in src
// that are missing from dst.
func mergeRoutes(src, dst RouteTable) {
	for k, v := range src {
		if _, ok := dst[k]; !ok {
			dst[k] = v
		}
	}
}

func (c Config) Undecoded() []string {
	return c.undecoded
}

func (c Config) String() string {
	s, err := c.ToString()
	if err != nil {
		return fmt.Sprintf("<error: %v>", err)
	}
	return s
}

func (c Config) ToString() (string, error) {
	buf := new(bytes.Buffer)
	if err := toml.NewEncoder(buf).Encode(c); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func Load(filename string) (*Config, error) {
	contents, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("error reading config file: %v", err)
	}
	return LoadString(string(contents))
}

func LoadString(contents string) (*Config, error) {
	conf := &Config{}
	md, err := toml.Decode(contents, conf)
	if err != nil {
		return nil, fmt.Errorf("error parsing config: %v", err)
	}

	// Save undecoded keys so they can be accessed later (e.g. for debugging
	// or checking).
	for _, key := range md.Undecoded() {
		conf.undecoded = append(conf.undecoded, strings.Join(key, "."))
	}

	// Link routes.
	for _, https := range conf.HTTPS {
		if https.RouteTable == nil {
			https.RouteTable = RouteTable{}
		}
		if https.BaseRoutes != "" {
			mergeRoutes(conf.Routes[https.BaseRoutes], https.RouteTable)
		}
	}
	for _, http := range conf.HTTP {
		if http.RouteTable == nil {
			http.RouteTable = RouteTable{}
		}
		if http.BaseRoutes != "" {
			mergeRoutes(conf.Routes[http.BaseRoutes], http.RouteTable)
		}
	}

	return conf, nil
}
