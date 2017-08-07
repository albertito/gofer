package main

import (
	"flag"
	"net/http"
	"runtime"
	"time"

	"blitiri.com.ar/go/gofer/config"
	"blitiri.com.ar/go/gofer/proxy"
	"blitiri.com.ar/go/gofer/util"
)

// Flags.
var (
	configfile = flag.String("configfile", "gofer.conf",
		"Configuration file")
)

func main() {
	flag.Parse()
	util.InitLog()

	conf, err := config.Load(*configfile)
	if err != nil {
		util.Log.Fatalf("error reading config file: %v", err)
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

	// Monitoring server.
	if conf.ControlAddr != "" {
		mux := http.NewServeMux()
		mux.HandleFunc("/debug/stack", dumpStack)
		mux.HandleFunc("/debug/config", dumpConfigFunc(conf))

		server := http.Server{
			Addr:     conf.ControlAddr,
			ErrorLog: util.Log,
			Handler:  mux,
		}

		util.Log.Printf("%s Starting monitoring server ", server.Addr)
		util.Log.Fatal(server.ListenAndServe())
	} else {
		util.Log.Print("No monitoring server, idle loop")
		time.Sleep(1 * time.Hour)
	}
}

// dumpStack handler for the control listener.
func dumpStack(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	buf := make([]byte, 500*1024)
	c := runtime.Stack(buf, true)
	w.Write(buf[:c])
}

// dumpConfig handler for the control listener.
func dumpConfigFunc(conf *config.Config) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, err := conf.ToString()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(s))
	})
}
