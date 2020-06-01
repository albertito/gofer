// Package config implements the gofer configuration.
package config

import (
	"fmt"
	"io/ioutil"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ControlAddr string `yaml:"control_addr"`

	// Map address -> config.
	HTTP  map[string]HTTP
	HTTPS map[string]HTTPS
	Raw   map[string]Raw
}

type HTTP struct {
	Proxy    map[string]string
	Dir      map[string]string
	Static   map[string]string
	Redirect map[string]string
	CGI      map[string]string
}

type HTTPS struct {
	HTTP  `yaml:",inline"`
	Certs string
}

type Raw struct {
	Addr  string
	Certs string
	To    string
	ToTLS bool `yaml:"to_tls"`
}

func (c Config) String() string {
	s, err := c.ToString()
	if err != nil {
		return fmt.Sprintf("<error: %v>", err)
	}
	return s
}

func (c Config) ToString() (string, error) {
	d, err := yaml.Marshal(&c)
	return string(d), err
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
	err := yaml.Unmarshal([]byte(contents), conf)
	return conf, err
}
