package main

import (
	"flag"
	"time"

	"blitiri.com.ar/go/gofer/config"
	"blitiri.com.ar/go/gofer/debug"
	"blitiri.com.ar/go/gofer/server"
	"blitiri.com.ar/go/log"
)

// Flags.
var (
	configfile = flag.String("configfile", "gofer.yaml", "Configuration file")
)

func main() {
	flag.Parse()
	log.Init()

	conf, err := config.Load(*configfile)
	if err != nil {
		log.Fatalf("error reading config file: %v", err)
	}

	for addr, https := range conf.HTTPS {
		go server.HTTPS(addr, https)
	}

	for addr, http := range conf.HTTP {
		go server.HTTP(addr, http)
	}

	for addr, raw := range conf.Raw {
		go server.Raw(addr, raw)
	}

	if conf.ControlAddr != "" {
		go debug.ServeDebugging(conf.ControlAddr, conf)
	}

	for {
		time.Sleep(1 * time.Hour)
	}
}
