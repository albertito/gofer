package main

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"blitiri.com.ar/go/gofer/config"
	"blitiri.com.ar/go/gofer/debug"
)

func TestDumpConfig(t *testing.T) {
	conf, err := config.Load("gofer.conf.example")
	if err != nil {
		t.Fatalf("error loading config example: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(debug.DumpConfigFunc(conf)))

	res, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("\n----- 8< -----\n%s\n----- 8< -----\n", body)
	if !strings.Contains(string(body), "localhost") {
		t.Errorf("expected body to contain 'localhost'")
	}
}
