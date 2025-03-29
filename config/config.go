// Package config implements the gofer configuration.
package config

import (
	"fmt"
	"io/ioutil"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ControlAddr string `yaml:"control_addr,omitempty"`

	// Map address -> config.
	HTTP  map[string]HTTP  `yaml:",omitempty"`
	HTTPS map[string]HTTPS `yaml:",omitempty"`
	Raw   map[string]Raw   `yaml:",omitempty"`

	ReqLog map[string]ReqLog `yaml:",omitempty"`

	RateLimit map[string]RateLimit `yaml:",omitempty"`
}

type HTTP struct {
	Routes map[string]Route

	Auth map[string]string `yaml:",omitempty"`

	SetHeader map[string]map[string]string `yaml:",omitempty"`

	ReqLog map[string]string `yaml:",omitempty"`

	RateLimit map[string]string `yaml:",omitempty"`

	Timeouts map[string]Timeout `yaml:",omitempty"`
}

type HTTPS struct {
	HTTP      `yaml:",inline"`
	Certs     string    `yaml:",omitempty"`
	AutoCerts AutoCerts `yaml:"autocerts,omitempty"`

	// Where to write key log files for debugging TLS.
	InsecureKeyLogFile string `yaml:"insecure_key_log_file,omitempty"`
}

type AutoCerts struct {
	Hosts    []string `yaml:",omitempty"`
	CacheDir string   `yaml:",omitempty"`
	Email    string   `yaml:",omitempty"`
	AcmeURL  string   `yaml:",omitempty"`
}

type Timeout struct {
	Read  time.Duration `yaml:",omitempty"`
	Write time.Duration `yaml:",omitempty"`
}

type Route struct {
	Dir        string   `yaml:",omitempty"`
	File       string   `yaml:",omitempty"`
	Proxy      *URL     `yaml:",omitempty"`
	Redirect   *URL     `yaml:",omitempty"`
	RedirectRe []RePair `yaml:"redirect_re,omitempty"`
	CGI        []string `yaml:",omitempty"`
	Status     int      `yaml:",omitempty"`
	DirOpts    DirOpts  `yaml:",omitempty"`
}

type DirOpts struct {
	Listing map[string]bool `yaml:",omitempty"`
	Exclude []PathRegexp    `yaml:",omitempty"`
}

type Raw struct {
	Certs     string `yaml:",omitempty"`
	To        string `yaml:",omitempty"`
	ToTLS     bool   `yaml:"to_tls,omitempty"`
	ReqLog    string `yaml:",omitempty"`
	RateLimit string `yaml:",omitempty"`
}

type ReqLog struct {
	File    string `yaml:",omitempty"`
	BufSize int    `yaml:",omitempty"`
	Format  string `yaml:",omitempty"`
}

type RateLimit struct {
	Rate Rate `yaml:",omitempty"`
	Size int  `yaml:",omitempty"`

	Rate64 Rate `yaml:",omitempty"`
	Rate56 Rate `yaml:",omitempty"`
	Rate48 Rate `yaml:",omitempty"`
}

type RePair struct {
	From   *regexp.Regexp
	To     string
	Status int
}

func (c Config) String() string {
	d, err := yaml.Marshal(&c)
	if err != nil {
		return fmt.Sprintf("<error: %v>", err)
	}
	return string(d)
}

func (c Config) Check() []error {
	errs := []error{}
	for addr, h := range c.HTTP {
		errs = append(errs, h.Check(c, addr)...)

	}

	for addr, h := range c.HTTPS {
		errs = append(errs, h.Check(c, addr)...)

		// For HTTPS, either Certs or AutoCerts must be set.
		if h.Certs == "" && len(h.AutoCerts.Hosts) == 0 {
			errs = append(errs,
				fmt.Errorf("%q: certs or autocerts must be set", addr))
		}
	}

	for addr, r := range c.Raw {
		if _, ok := c.ReqLog[r.ReqLog]; r.ReqLog != "" && !ok {
			errs = append(errs,
				fmt.Errorf("%q: unknown reqlog %q", addr, r.ReqLog))
		}
		if _, ok := c.RateLimit[r.RateLimit]; r.RateLimit != "" && !ok {
			errs = append(errs,
				fmt.Errorf("%q: unknown ratelimit %q", addr, r.RateLimit))
		}
	}

	return errs
}

func (h HTTP) Check(c Config, addr string) []error {
	errs := []error{}

	if len(h.Routes) == 0 {
		errs = append(errs, fmt.Errorf("%q: missing routes", addr))
	}

	for path, r := range h.Routes {
		if len(r.DirOpts.Listing)+len(r.DirOpts.Exclude) > 0 && r.Dir == "" {
			errs = append(errs,
				fmt.Errorf("%q: %q: diropts is set on non-dir route",
					addr, path))
		}

		nSet := nTrue(
			r.Dir != "",
			r.File != "",
			r.Proxy != nil,
			r.Redirect != nil,
			len(r.RedirectRe) > 0,
			len(r.CGI) > 0,
			r.Status > 0)
		if nSet > 1 {
			errs = append(errs,
				fmt.Errorf("%q: %q: too many actions set", addr, path))
		} else if nSet == 0 {
			errs = append(errs,
				fmt.Errorf("%q: %q: action missing", addr, path))
		}
	}

	for path, name := range h.ReqLog {
		if _, ok := c.ReqLog[name]; !ok {
			errs = append(errs,
				fmt.Errorf("%q: %q: unknown reqlog %q", addr, path, name))
		}
	}
	for path, name := range h.RateLimit {
		if _, ok := c.RateLimit[name]; !ok {
			errs = append(errs,
				fmt.Errorf("%q: %q: unknown ratelimit %q", addr, path, name))
		}
	}

	// Verify timeouts are positive.
	for path, timeout := range h.Timeouts {
		if timeout.Read < 0 {
			errs = append(errs,
				fmt.Errorf("%q: %q: read timeout must be positive", addr, path))
		}
		if timeout.Write < 0 {
			errs = append(errs,
				fmt.Errorf("%q: %q: write timeout must be positive", addr, path))
		}
	}

	return errs
}

// Count how many true values are in a series of bools.
func nTrue(bs ...bool) int {
	n := 0
	for _, b := range bs {
		if b {
			n++
		}
	}
	return n
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

	rx, err := regexp.Compile(s)
	if err != nil {
		return err
	}

	re.Regexp = rx
	return nil
}

func (re Regexp) MarshalYAML() (interface{}, error) {
	return re.String(), nil
}

// Wrapper to simplify regexp in configuration. This is specifically for use
// on regexp paths, which are always anchored to the beginning and end of the
// string for ease of use.
type PathRegexp struct {
	orig string
	*regexp.Regexp
}

func (re *PathRegexp) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}

	rx, err := regexp.Compile("^(?:" + s + ")$")
	if err != nil {
		return err
	}

	re.orig = s
	re.Regexp = rx
	return nil
}

func (re PathRegexp) MarshalYAML() (interface{}, error) {
	return re.orig, nil
}

// Wrapper to simplify URLs in configuration.
type URL url.URL

func (u *URL) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}

	x, err := url.Parse(s)
	if err != nil {
		return err
	}

	*u = URL(*x)
	return nil
}

func (u *URL) MarshalYAML() (interface{}, error) {
	if u == nil {
		return "", nil
	}
	return u.String(), nil
}

func (u *URL) URL() url.URL {
	return url.URL(*u)
}

func (u URL) String() string {
	p := u.URL()
	return p.String()
}

// Rate type to simplify rate limits in configuration.
// Format is "requests/period", e.g. "10/1s".
type Rate struct {
	Requests uint64
	Period   time.Duration
}

func (r *Rate) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}

	sp := strings.SplitN(s, "/", 2)
	if len(sp) != 2 {
		return fmt.Errorf("invalid rate format %q (needs a single '/')", s)
	}
	reqS, periodS := strings.TrimSpace(sp[0]), strings.TrimSpace(sp[1])

	req, err := strconv.ParseUint(reqS, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid requests in %q: %v", s, err)
	}

	period, err := time.ParseDuration(periodS)
	if err != nil {
		return fmt.Errorf("invalid period in %q: %v", s, err)
	}
	if period == 0 {
		return fmt.Errorf("period must be >0 in %q", s)
	}

	r.Requests = req
	r.Period = period

	return nil
}

func (r Rate) MarshalYAML() (interface{}, error) {
	return fmt.Sprintf("%d/%s", r.Requests, r.Period), nil
}
