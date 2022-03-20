package debug

import (
	"html/template"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"time"

	// Remote profiling support.
	_ "net/http/pprof"

	"blitiri.com.ar/go/gofer/config"
	"blitiri.com.ar/go/gofer/nettrace"
	"blitiri.com.ar/go/log"
)

// Build information, overridden at build time using
// -ldflags="-X blitiri.com.ar/go/gofer/debug.Version=blah".
var (
	Version      = "undefined"
	SourceDateTs = "0"

	// Derived from SourceDateTs.
	SourceDate    = time.Time{}
	SourceDateStr = ""
)

func init() {
	sdts, err := strconv.ParseInt(SourceDateTs, 10, 0)
	if err != nil {
		panic(err)
	}

	SourceDate = time.Unix(sdts, 0)
	SourceDateStr = SourceDate.Format("2006-01-02 15:04:05 -0700")
}

func ServeDebugging(addr string, conf *config.Config) {
	log.Infof("debugging HTTP server listening on %q", addr)

	indexData := struct {
		Version    string
		GoVersion  string
		SourceDate time.Time
		StartTime  time.Time
		Args       []string
	}{
		Version:    Version,
		GoVersion:  runtime.Version(),
		SourceDate: SourceDate,
		StartTime:  time.Now(),
		Args:       os.Args,
	}

	http.HandleFunc("/debug/config", DumpConfigFunc(conf))
	nettrace.RegisterHandler(http.DefaultServeMux)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if err := htmlIndex.Execute(w, indexData); err != nil {
			log.Infof("Monitoring handler error: %v", err)
		}
	})

	err := http.ListenAndServe(addr, nil)
	log.Errorf("debugging HTTP server died: %v", err)
}

func DumpConfigFunc(conf *config.Config) http.HandlerFunc {
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

// Static index for the debugging website.
var htmlIndex = template.Must(template.New("index").Funcs(
	template.FuncMap{"since": time.Since}).Parse(
	`<!DOCTYPE html>
<html>
  <head>
    <title>gofer debugging</title>
  </head>
  <body>
    <h1>gofer debugging</h1>

	version {{.Version}}<br>
	source date {{.SourceDate.Format "2006-01-02 15:04:05 -0700"}}<br>
	built with: {{.GoVersion}}<p>

	started {{.StartTime.Format "Mon, 2006-01-02 15:04:05 -0700"}}<br>
	up for {{.StartTime | since}}<p>

	args: <tt>{{.Args}}</tt><p>

    <ul>
      <li><a href="/debug/config">configuration</a>
      <li><a href="/debug/traces">traces</a>
      <li><a href="/debug/vars">exported variables</a>
	       <small><a href="https://golang.org/pkg/expvar/">(ref)</a></small>
      <li><a href="/debug/pprof">pprof</a>
          <small><a href="https://golang.org/pkg/net/http/pprof/">
            (ref)</a></small>
        <ul>
          <li><a href="/debug/pprof/goroutine?debug=1">goroutines</a>
        </ul>
    </ul>
  </body>
</html>
`))
