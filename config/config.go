// Package config implements the gofer configuration.
package config

import (
	"fmt"
	"io/ioutil"
	"regexp"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ControlAddr string `yaml:"control_addr"`

	// Map address -> config.
	HTTP  map[string]HTTP
	HTTPS map[string]HTTPS
	Raw   map[string]Raw

	ReqLog map[string]ReqLog
}

type HTTP struct {
	Proxy    map[string]string
	Dir      map[string]string
	File     map[string]string
	Redirect map[string]string
	CGI      map[string]string
	Status   map[string]int

	Auth map[string]string

	DirOpts   map[string]DirOpts
	SetHeader map[string]map[string]string

	ReqLog map[string]string
}

type HTTPS struct {
	HTTP  `yaml:",inline"`
	Certs string
}

type DirOpts struct {
	Listing map[string]bool
	Exclude []Regexp
}

type Raw struct {
	Addr   string
	Certs  string
	To     string
	ToTLS  bool `yaml:"to_tls"`
	ReqLog string
}

type ReqLog struct {
	File    string
	BufSize int
	Format  string
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

// Wrapper to simplify regexp in configuration.
type Regexp struct {
	*regexp.Regexp
}

func (re *Regexp) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}

	rx, err := regexp.Compile("^(?:" + s + ")$")
	if err != nil {
		return err
	}

	re.Regexp = rx
	return nil
}
