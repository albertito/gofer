package debug

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"runtime"
	"runtime/debug"
	"time"

	// Remote profiling support.
	_ "net/http/pprof"

	"blitiri.com.ar/go/gofer/config"
	"blitiri.com.ar/go/gofer/nettrace"
	"blitiri.com.ar/go/gofer/ratelimit"
	"blitiri.com.ar/go/log"
)

var (
	Version    = ""
	SourceDate = time.Time{}
)

func init() {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		panic("unable to read build info")
	}

	dirty := false
	gitRev := ""
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.modified":
			if s.Value == "true" {
				dirty = true
			}
		case "vcs.time":
			SourceDate, _ = time.Parse(time.RFC3339, s.Value)
		case "vcs.revision":
			gitRev = s.Value
		}
	}

	Version = SourceDate.Format("20060102")
	if gitRev != "" {
		Version += fmt.Sprintf("-%.9s", gitRev)
	}
	if dirty {
		Version += "-dirty"
	}
}

func ServeDebugging(addr string, conf *config.Config) error {
	hostname, _ := os.Hostname()

	indexData := struct {
		Hostname   string
		Version    string
		GoVersion  string
		SourceDate time.Time
		StartTime  time.Time
		Args       []string
	}{
		Hostname:   hostname,
		Version:    Version,
		GoVersion:  runtime.Version(),
		SourceDate: SourceDate,
		StartTime:  time.Now(),
		Args:       os.Args,
	}

	http.HandleFunc("/debug/config", DumpConfigFunc(conf))
	http.HandleFunc("/debug/ratelimit", ratelimit.DebugHandler)
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

	log.Infof("debugging HTTP server listening on %q", addr)
	err := http.ListenAndServe(addr, nil)
	return log.Errorf("debugging HTTP server died: %v", err)
}

func DumpConfigFunc(conf *config.Config) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(conf.String()))
	})
}

// Functions available inside the templates.
var tmplFuncs = template.FuncMap{
	"since": time.Since,
	"roundDuration": func(d time.Duration) time.Duration {
		return d.Round(time.Second)
	},
}

// Static index for the debugging website.
var htmlIndex = template.Must(
	template.New("index").Funcs(tmplFuncs).Parse(
		`<!DOCTYPE html>
<html>

<head>
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Hostname}}: gofer debugging</title>
<style type="text/css">
  body {
    font-family: sans-serif;
  }
  @media (prefers-color-scheme: dark) {
    body {
      background: #121212;
	  color: #c9d1d9;
	}
	a { color: #44b4ec; }
  }
</style>
</head>

<body>
  <h1>gofer @{{.Hostname}}</h1>

  version {{.Version}}<br>
  source date {{.SourceDate.Format "2006-01-02 15:04:05 -0700"}}<br>
  built with: {{.GoVersion}}<p>

  started {{.StartTime.Format "Mon, 2006-01-02 15:04:05 -0700"}}<br>
  up for {{.StartTime | since | roundDuration}}<br>
  os hostname <i>{{.Hostname}}</i><br>
  <p>

  args: <tt>{{.Args}}</tt><p>

  <ul>
    <li><a href="/debug/config">configuration</a>
    <li><a href="/debug/traces">traces</a>
    <li><a href="/debug/ratelimit">ratelimit</a>
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
