package main

import (
	"flag"
	"time"

	"blitiri.com.ar/go/gofer/config"
	"blitiri.com.ar/go/gofer/debug"
	"blitiri.com.ar/go/gofer/proxy"
	"blitiri.com.ar/go/log"
)

// Flags.
var (
	configfile = flag.String("configfile", "gofer.conf",
		"Configuration file")
)

func main() {
	flag.Parse()
	log.Init()

	conf, err := config.Load(*configfile)
	if err != nil {
		log.Fatalf("error reading config file: %v", err)
	}

	for _, k := range conf.Undecoded() {
		log.Infof("warning: undecoded config key: %q", k)
	}

	for _, https := range conf.HTTPS {
		go proxy.HTTPS(*https)
	}

	for _, http := range conf.HTTP {
		go proxy.HTTP(*http)
	}

	for _, raw := range conf.Raw {
		go proxy.Raw(raw)
	}

	if conf.ControlAddr != "" {
		go debug.ServeDebugging(conf.ControlAddr, conf)
	}

	for {
		time.Sleep(1 * time.Hour)
	}
}
