package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	golog "log"
	"net/http"
	"net/http/cgi"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"blitiri.com.ar/go/gofer/config"
	"blitiri.com.ar/go/gofer/trace"
	"blitiri.com.ar/go/gofer/util"
	"blitiri.com.ar/go/log"
	"blitiri.com.ar/go/systemd"
)

func httpServer(addr string, conf config.HTTP) *http.Server {
	ev := trace.NewEventLog("httpserver", addr)

	srv := &http.Server{
		Addr: addr,

		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,

		ErrorLog: golog.New(ev, "", golog.Lshortfile),
	}

	// Load route table.
	mux := http.NewServeMux()
	srv.Handler = mux

	routes := []struct {
		name        string
		table       map[string]string
		makeHandler func(string, url.URL) http.Handler
	}{
		{"proxy", conf.Proxy, makeProxy},
		{"dir", conf.Dir, makeDir},
		{"static", conf.Static, makeStatic},
		{"redirect", conf.Redirect, makeRedirect},
		{"cgi", conf.CGI, makeCGI},
	}
	for _, r := range routes {
		for from, to := range r.table {
			toURL, err := url.Parse(to)
			if err != nil {
				log.Fatalf(
					"route %s %q -> %q: destination is not a valid URL: %v",
					r.name, from, to, err)
			}
			log.Infof("%s route %q -> %s %q", srv.Addr, from, r.name, toURL)
			mux.Handle(from, r.makeHandler(from, *toURL))
		}
	}

	return srv
}

func HTTP(addr string, conf config.HTTP) {
	srv := httpServer(addr, conf)
	lis, err := systemd.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("%s error listening: %v", addr, err)
	}
	log.Infof("%s http proxy starting on %q", addr, lis.Addr())
	err = srv.Serve(lis)
	log.Fatalf("%s http proxy exited: %v", addr, err)
}

func HTTPS(addr string, conf config.HTTPS) {
	var err error
	srv := httpServer(addr, conf.HTTP)

	srv.TLSConfig, err = util.LoadCerts(conf.Certs)
	if err != nil {
		log.Fatalf("%s error loading certs: %v", addr, err)
	}

	rawLis, err := systemd.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("%s error listening: %v", addr, err)
	}

	// We need to set the NextProtos manually before creating the TLS
	// listener, the library cannot help us with this.
	srv.TLSConfig.NextProtos = append(srv.TLSConfig.NextProtos,
		"h2", "http/1.1")
	lis := tls.NewListener(rawLis, srv.TLSConfig)

	log.Infof("%s https proxy starting on %q", addr, lis.Addr())
	err = srv.Serve(lis)
	log.Fatalf("%s https proxy exited: %v", addr, err)
}

func makeProxy(from string, to url.URL) http.Handler {
	proxy := &httputil.ReverseProxy{}
	proxy.Transport = transport

	// Director that strips "from" from the request path, so that if we have
	// this config:
	//
	//   /a/ -> http://dst/b
	//   www.example.com/p/ -> http://dst/q
	//
	// then:
	//   /a/x  goes to  http://dst/b/x (not http://dst/b/a/x)
	//   www.example.com/p/x  goes to  http://dst/q/x

	// Strip the domain from `from`, if any. That is useful for the http
	// router, but to us is irrelevant.
	from = stripDomain(from)

	proxy.Director = func(req *http.Request) {
		req.URL.Scheme = to.Scheme
		req.URL.Host = to.Host
		req.URL.RawQuery = req.URL.RawQuery
		req.URL.Path = adjustPath(req.URL.Path, from, to.Path)

		// If the user agent is not set, prevent a fall back to the default value.
		if _, ok := req.Header["User-Agent"]; !ok {
			req.Header.Set("User-Agent", "")
		}

		// Note we don't do this so we can have routes independent of virtual
		// hosts. The downside is that if the destination scheme is HTTPS,
		// this causes issues with the TLS SNI negotiation.
		//req.Host = to.Host
	}

	return newReverseProxy(proxy)
}

// joinPath joins to HTTP paths. We can't use path.Join because it strips the
// final "/", which may have meaning in URLs.
func joinPath(a, b string) string {
	if a == "" && b == "" {
		return "/"
	}
	if a == "" || b == "" {
		return a + b
	}
	if strings.HasSuffix(a, "/") && strings.HasPrefix(b, "/") {
		return strings.TrimSuffix(a, "/") + b
	}
	if !strings.HasSuffix(a, "/") && !strings.HasPrefix(b, "/") {
		return a + "/" + b
	}

	return a + b
}

func stripDomain(from string) string {
	// Strip the domain from `from`, if any. That is useful for the http
	// router, but to us is irrelevant.
	if idx := strings.Index(from, "/"); idx > 0 {
		from = from[idx:]
	}
	return from
}

func adjustPath(req string, from string, to string) string {
	// Strip "from" from the request path, so that if we have this config:
	//
	//   /a/ -> http://dst/b
	//   /p/q -> http://dst/r/s
	//   www.example.com/t/ -> http://dst/u
	//
	// then:
	//   /a/x  goes to  http://dst/b/x (not http://dst/b/a/x)
	//   /p/q  goes to  http://dst/r/s
	//   www.example.com/t/x  goes to  http://dst/u/x
	//
	// It is expected that `from` already has the domain removed using
	// stripDomain.
	//
	// If req doesn't have from as prefix, then we panic.
	if !strings.HasPrefix(req, from) {
		panic(fmt.Errorf(
			"adjustPath(req=%q, from=%q, to=%q): from is not prefix",
			req, from, to))
	}

	dst := joinPath(to, strings.TrimPrefix(req, from))
	if dst == "" || dst[0] != '/' {
		dst = "/" + dst
	}
	return dst
}

func pathOrOpaque(u url.URL) string {
	if u.Path != "" {
		return u.Path
	}

	// This happens for relative paths, which are fine in this context.
	return u.Opaque
}

func makeDir(from string, to url.URL) http.Handler {
	from = stripDomain(from)
	path := pathOrOpaque(to)

	fs := http.FileServer(http.Dir(path))
	return WithLogging("http:dir",
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tr, _ := trace.FromContext(r.Context())
			tr.Printf("serving dir root %q", path)

			r.URL.Path = strings.TrimPrefix(r.URL.Path, from)
			if r.URL.Path == "" || r.URL.Path[0] != '/' {
				r.URL.Path = "/" + r.URL.Path
			}
			tr.Printf("adjusted path: %q", r.URL.Path)
			fs.ServeHTTP(w, r)
		}),
	)
}

func makeStatic(from string, to url.URL) http.Handler {
	path := pathOrOpaque(to)

	return WithLogging("http:static",
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tr, _ := trace.FromContext(r.Context())
			tr.Printf("statically serving %q", path)
			http.ServeFile(w, r, path)
		}),
	)
}

func makeCGI(from string, to url.URL) http.Handler {
	from = stripDomain(from)
	path := pathOrOpaque(to)
	args := queryToArgs(to.RawQuery)

	return WithLogging("http:cgi",
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tr, _ := trace.FromContext(r.Context())
			tr.Debugf("exec %q %q", path, args)
			h := cgi.Handler{
				Path:   path,
				Args:   args,
				Root:   from,
				Logger: golog.New(tr, "", golog.Lshortfile),
				Stderr: tr,
			}
			h.ServeHTTP(w, r)
		}),
	)
}

func queryToArgs(query string) []string {
	args := []string{}
	for query != "" {
		comp := query
		if i := strings.IndexAny(comp, "&;"); i >= 0 {
			comp, query = comp[:i], comp[i+1:]
		} else {
			query = ""
		}

		comp, _ = url.QueryUnescape(comp)
		args = append(args, comp)

	}

	return args
}

func makeRedirect(from string, to url.URL) http.Handler {
	from = stripDomain(from)

	return WithLogging("http:redirect",
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tr, _ := trace.FromContext(r.Context())
			target := to
			target.RawQuery = r.URL.RawQuery
			target.Path = adjustPath(r.URL.Path, from, to.Path)
			tr.Printf("redirect to %q", target.String())

			http.Redirect(w, r, target.String(), http.StatusTemporaryRedirect)
		}),
	)
}

type loggingTransport struct{}

func (t *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tr, _ := trace.FromContext(req.Context())

	tr.Printf("proxy to: %s %s %s",
		req.Proto, req.Method, req.URL.String())

	response, err := http.DefaultTransport.RoundTrip(req)
	if err == nil {
		tr.Printf("%s", response.Status)
		tr.Printf("%d bytes", response.ContentLength)
		if response.StatusCode >= 400 && response.StatusCode != 404 {
			tr.SetError()
		}
	} else {
		// errorHandler will be invoked when err != nil, avoid double error
		// logging.
	}

	return response, err
}

// Use a single logging transport, we don't need more than one.
var transport = &loggingTransport{}

type reverseProxy struct {
	rp *httputil.ReverseProxy
}

func newReverseProxy(rp *httputil.ReverseProxy) http.Handler {
	p := &reverseProxy{
		rp: rp,
	}
	rp.ErrorHandler = p.errorHandler
	return p
}

func (p *reverseProxy) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	tr := trace.New("http:proxy", req.Host+req.URL.String())
	defer tr.Finish()

	tr.Printf("%s %s %s %s %s",
		req.RemoteAddr, req.Proto, req.Method, req.Host, req.URL.String())

	// Associate the trace with this request.
	req = req.WithContext(trace.NewContext(req.Context(), tr))

	p.rp.ServeHTTP(rw, req)
}

func (p *reverseProxy) errorHandler(w http.ResponseWriter, r *http.Request, err error) {
	tr, _ := trace.FromContext(r.Context())
	tr.Printf("backend error: %v", err)

	// Mark it as an error, unless it was context.Canceled, which is normal:
	// the client side has closed the connection.
	if !errors.Is(err, context.Canceled) {
		tr.SetError()
	}

	w.WriteHeader(http.StatusBadGateway)
}

// Wrapper around http.ResponseWriter so we can extract status and length.
type statusWriter struct {
	http.ResponseWriter
	status int
	length int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.length += n
	return n, err
}

func WithLogging(name string, parent http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tr := trace.New(name, r.Host+r.URL.String())
		defer tr.Finish()

		// Associate the trace with this request.
		r = r.WithContext(trace.NewContext(r.Context(), tr))

		// Wrap the writer so we can get output information.
		sw := statusWriter{ResponseWriter: w}

		tr.Printf("%s %s %s %s %s",
			r.RemoteAddr, r.Proto, r.Method, r.Host, r.URL.String())
		parent.ServeHTTP(&sw, r)
		tr.Printf("%d %s", sw.status, http.StatusText(sw.status))
		tr.Printf("%d bytes", sw.length)

		if sw.status >= 400 && sw.status != 404 {
			tr.SetError()
		}
	})
}