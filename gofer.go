package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"blitiri.com.ar/go/gofer/config"
	"blitiri.com.ar/go/gofer/debug"
	"blitiri.com.ar/go/gofer/reqlog"
	"blitiri.com.ar/go/gofer/server"
	"blitiri.com.ar/go/log"
)

// Flags.
var (
	configFile  = flag.String("configfile", "gofer.yaml", "Configuration file")
	configCheck = flag.Bool("configcheck", false,
		"Check the configuration and exit afterwards")
	configPrint = flag.Bool("configprint", false,
		"Check the configuration, print it, and exit afterwards")
)

func main() {
	flag.Parse()
	log.Init()
	log.Infof("gofer starting (%s, %s)", debug.Version, debug.SourceDateStr)

	conf, err := config.Load(*configFile)
	if err != nil {
		log.Fatalf("error reading config file: %v", err)
	}

	if errs := conf.Check(); len(errs) > 0 {
		for _, err := range errs {
			log.Errorf("%v", err)
		}
		log.Fatalf("invalid configuration")
	}
	if *configPrint {
		s, err := conf.ToString()
		if err != nil {
			log.Fatalf("%v", err)
		}
		fmt.Print(s)
		return
	}
	if *configCheck {
		log.Infof("config ok")
		return
	}

	go signalHandler()

	for name, rlog := range conf.ReqLog {
		err := reqlog.FromConfig(name, rlog)
		if err != nil {
			log.Fatalf(err.Error())
		}
	}

	servers := []runnerFunc{}

	for addr, https := range conf.HTTPS {
		addr := addr
		https := https
		servers = append(servers, func() error {
			return server.HTTPS(addr, https)
		})
	}

	for addr, http := range conf.HTTP {
		addr := addr
		http := http
		servers = append(servers, func() error {
			return server.HTTP(addr, http)
		})
	}

	for addr, raw := range conf.Raw {
		addr := addr
		raw := raw
		servers = append(servers, func() error {
			return server.Raw(addr, raw)
		})
	}

	if conf.ControlAddr != "" {
		servers = append(servers, func() error {
			return debug.ServeDebugging(conf.ControlAddr, conf)
		})
	}

	err = runMany(servers...)
	log.Fatalf(err.Error())
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

type runnerFunc func() error

func runMany(fs ...runnerFunc) error {
	var err error
	mu := &sync.Mutex{}
	cond := sync.NewCond(mu)

	mu.Lock()

	for _, f := range fs {
		go func(f runnerFunc) {
			e := f()
			mu.Lock()
			err = e
			cond.Broadcast()
			mu.Unlock()
		}(f)
	}

	cond.Wait()

	return err
}
