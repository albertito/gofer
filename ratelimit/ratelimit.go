package ratelimit

import (
	"fmt"
	"net/http"
	"sort"

	"blitiri.com.ar/go/gofer/config"
	"blitiri.com.ar/go/gofer/ipratelimit"
	"blitiri.com.ar/go/gofer/trace"
	"blitiri.com.ar/go/log"
)

// Global registry for convenience.
// This is not pretty but it simplifies a lot of the handling for now.
var registry = map[string]*ipratelimit.Limiter{}

var traces = map[*ipratelimit.Limiter]*trace.Trace{}

func FromConfig(name string, conf config.RateLimit) {
	if conf.Size == 0 {
		conf.Size = 1000
	}

	rl := ipratelimit.NewLimiter(
		conf.Rate.Requests, conf.Rate.Period, conf.Size)

	// If config has custom IPv6 rates, use them.
	if conf.Rate64.Period > 0 {
		rl.SetIPv6s64Rate(conf.Rate64.Requests, conf.Rate64.Period)
	}
	if conf.Rate56.Period > 0 {
		rl.SetIPv6s56Rate(conf.Rate56.Requests, conf.Rate56.Period)
	}
	if conf.Rate48.Period > 0 {
		rl.SetIPv6s48Rate(conf.Rate48.Requests, conf.Rate48.Period)
	}

	registry[name] = rl
	traces[rl] = trace.New("ratelimit", name)
	traces[rl].SetMaxEvents(1000)

	log.Infof("ratelimit %q: %d/%s, size %d",
		name, conf.Rate.Requests, conf.Rate.Period, conf.Size)
	return
}

func FromName(name string) *ipratelimit.Limiter {
	return registry[name]
}

func Trace(rl *ipratelimit.Limiter) *trace.Trace {
	return traces[rl]
}

func DebugHandler(w http.ResponseWriter, r *http.Request) {
	names := []string{}
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Fprintf(w, `<!DOCTYPE html>
<html>

<head>
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>ratelimit</title>
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
  table {
    text-align: right;
  }
  th {
    text-align: center;
  }
  td, th {
    padding: 0.15em 0.5em;
  }
  td.ip {
    min-width: 10em;
	text-align: left;
	font-family: monospace;
  }
</style>
</head>

<body>
`)

	for _, name := range names {
		fmt.Fprintf(w, "<h1>%s</h1>\n\n%s\n\n",
			name, registry[name].DebugHTML())
	}

	fmt.Fprintf(w, "</body>\n</html>\n")
}
