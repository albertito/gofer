package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"blitiri.com.ar/go/gofer/config"
	"blitiri.com.ar/go/gofer/debug"
	"blitiri.com.ar/go/gofer/reqlog"
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
	log.Infof("gofer starting")

	conf, err := config.Load(*configfile)
	if err != nil {
		log.Fatalf("error reading config file: %v", err)
	}

	go signalHandler()

	for name, rlog := range conf.ReqLog {
		reqlog.FromConfig(name, rlog)
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

func signalHandler() {
	var err error

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGHUP)

	for {
		switch sig := <-signals; sig {
		case syscall.SIGHUP:
			// SIGHUP triggers a reopen of the log files. This is used for log
			// rotation.
			err = log.Default.Reopen()
			if err != nil {
				log.Fatalf("Error reopening log: %v", err)
			}

			reqlog.ReopenAll()
		default:
			log.Errorf("Unexpected signal %v", sig)
		}
	}
}
