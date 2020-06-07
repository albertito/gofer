package reqlog

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"text/template"
	"time"

	"blitiri.com.ar/go/gofer/config"
	"blitiri.com.ar/go/gofer/trace"
	"blitiri.com.ar/go/log"
)

type Log struct {
	path   string
	f      *os.File
	evs    chan *Event
	reopen chan bool
	tmpl   *template.Template

	tr *trace.EventLog
}

type Event struct {
	T time.Time
	H *http.Request
	R *RawRequest

	Status int
	Length int64

	Latency time.Duration
}

type RawRequest struct {
	RemoteAddr net.Addr
	LocalAddr  net.Addr
}

// Common log format, used by many servers.
// https://en.wikipedia.org/wiki/Common_Log_Format
// https://httpd.apache.org/docs/2.4/logs.html#common
const commonFormat = "{{.H.RemoteAddr}} - - [{{.T.Format \"02/Jan/2006:15:04:05 -0700\"}}] \"{{.H.Method}} {{.H.URL}} {{.H.Proto}}\" {{.Status}} {{.Length}}\n"

// Combined log format, extension of the Common Log Format, and used by a lot
// of servers (e.g. Apache).
// https://httpd.apache.org/docs/2.4/logs.html#combined
const combinedFormat = "{{.H.RemoteAddr}} - - [{{.T.Format \"02/Jan/2006:15:04:05 -0700\"}}] \"{{.H.Method}} {{.H.URL}} {{.H.Proto}}\" {{.Status}} {{.Length}} {{.H.Header.Referer|q}} {{.H.Header.User-agent|q}}\n"

// Extension of the combined log format, prepending the virtual host.
// https://httpd.apache.org/docs/2.4/logs.html#virtualhost
const combinedVHFormat = "{{.H.Host}} " + combinedFormat

// lighttpd log is like combined, but the virtual host is put instead of the
// ident field.
const lighttpdFormat = "{{.H.RemoteAddr}} {{.H.Host}} - [{{.T.Format \"02/Jan/2006:15:04:05 -0700\"}}] \"{{.H.Method}} {{.H.URL}} {{.H.Proto}}\" {{.Status}} {{.Length}}\n"

// gofer format, this is the default, and can handle both raw and HTTP events.
const goferFormat = "{{.T.Format \"2006-01-02 15:04:05.000\"}}" +
	"{{if .H}} {{.H.RemoteAddr}} {{.H.Proto}} {{.H.Host}} {{.H.Method}} {{.H.URL}}{{end}}" +
	"{{if .R}} {{.R.RemoteAddr}} raw {{.R.LocalAddr}}{{end}}" +
	" = {{.Status}} {{.Length}}b {{.Latency.Milliseconds}}ms\n"

var knownFormats = map[string]string{
	"<common>":     commonFormat,
	"<combined>":   combinedFormat,
	"<combinedvh>": combinedVHFormat,
	"<lighttpd>":   lighttpdFormat,
	"<gofer>":      goferFormat,
	"":             goferFormat,
}

func New(path string, nbuf int, format string) (*Log, error) {
	var err error
	h := &Log{}

	if f, ok := knownFormats[format]; ok {
		format = f
	}
	h.tmpl = template.New(path)
	h.tmpl.Funcs(template.FuncMap{
		"q": quoteString,
	})
	_, err = h.tmpl.Parse(format)
	if err != nil {
		return nil, err
	}

	switch path {
	case "<stdout>":
		h.f = os.Stdout
	case "<stderr>":
		h.f = os.Stderr
	default:
		// TODO: stdout/stderr (and their reopen).
		h.f, err = os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return nil, err
		}
		h.path = path
	}

	h.evs = make(chan *Event, nbuf)
	h.reopen = make(chan bool, 1)
	h.tr = trace.NewEventLog("reqlog", path)

	go h.run()
	return h, nil
}

func (h *Log) run() {
	var err error
	for {
		select {
		case e := <-h.evs:
			err = h.tmpl.Execute(h.f, e)
			if err != nil {
				h.tr.Errorf("error logging: %v", err)
			}
		case <-h.reopen:
			if h.path != "" {
				h.f.Close()
				h.f, err = os.OpenFile(
					h.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
				if err != nil {
					h.tr.Errorf("error reopening: %v", err)
				}
			}
		}
	}
}

func (h *Log) Log(e *Event) {
	h.evs <- e
}

func (h *Log) Reopen() {
	h.reopen <- true
}

// Global registry for convenience.
// This is not pretty but it simplifies a lot of the handling for now.
var registry = map[string]*Log{}

func FromConfig(name string, conf config.ReqLog) error {
	h, err := New(conf.File, conf.BufSize, conf.Format)
	if err != nil {
		log.Fatalf("reqlog %q failed to initialize: %v", name, err)
		return err
	}
	registry[name] = h
	log.Infof("reqlog %q writing to %q", name, conf.File)
	return nil
}

func FromName(name string) *Log {
	return registry[name]
}

type ctxKeyT string

const ctxKey = ctxKeyT("reqlog")

func NewContext(ctx context.Context, log *Log) context.Context {
	return context.WithValue(ctx, ctxKey, log)
}
func FromContext(ctx context.Context) *Log {
	v := ctx.Value(ctxKey)
	if v == nil {
		return nil
	}
	return v.(*Log)
}

func quoteString(s string) string {
	return fmt.Sprintf("%q", s)
}
