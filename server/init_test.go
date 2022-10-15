package server

import (
	"strings"
	"testing"

	"blitiri.com.ar/go/gofer/config"
)

// Tests that cover errors in the initialization of the servers.

const baseConfig = `
http:
  ":80":
    routes:
      "/": { dir: "./testdata/" }

https:
  ":443":
    autocerts:
      hosts: ["http-test"]
      acmeurl: "http://localhost/invalid/just/in/case"
    routes:
      "/": { dir: "./testdata/" }

raw:
  ":1000":
    to: "localhost:2000"
`

func mustLoadConfig(t *testing.T, s string) *config.Config {
	t.Helper()
	conf, err := config.LoadString(s)
	if err != nil {
		t.Fatalf("error loading test config: %v", err)
	}
	return conf
}

func expectErr(t *testing.T, err error, s string) {
	t.Helper()

	if err == nil {
		t.Errorf("expected error %q, got nil", s)
		return
	}

	if !strings.Contains(err.Error(), s) {
		t.Errorf("expected error %q, got %v", s, err)
	}
}

func TestBadPort(t *testing.T) {
	conf := mustLoadConfig(t, baseConfig)

	err := HTTP(":badport", conf.HTTP[":80"])
	expectErr(t, err, "error listening")

	err = HTTPS(":badport", conf.HTTPS[":443"])
	expectErr(t, err, "error listening")

	err = Raw(":badport", conf.Raw[":1000"])
	expectErr(t, err, "error listening")
}

func TestCertsNotFound(t *testing.T) {
	conf := mustLoadConfig(t, baseConfig)
	hconf := conf.HTTPS[":443"]
	hconf.Certs = "./not/found"

	err := HTTPS(":badport", hconf)
	expectErr(t, err, "no such file or directory")

	rconf := conf.Raw[":1000"]
	rconf.Certs = "./not/found"
	err = Raw(":badport", rconf)
	expectErr(t, err, "no such file or directory")
}

func TestAuthFileNotFound(t *testing.T) {
	conf := mustLoadConfig(t, baseConfig)
	hconf := conf.HTTP[":80"]
	hconf.Auth = map[string]string{
		"/": "./not/found",
	}

	err := HTTP(":badport", hconf)
	expectErr(t, err, "failed to load auth file")
}

func TestReqLogNotFound(t *testing.T) {
	conf := mustLoadConfig(t, baseConfig)

	// Use HTTPS this time, for variety, and also to check that httpServer
	// errors get properly propagated for it too.
	hconf := conf.HTTPS[":443"]
	hconf.ReqLog = map[string]string{
		"/api/": "unk-reqlog",
	}

	err := HTTPS(":badport", hconf)
	expectErr(t, err, "unknown reqlog name")
}
