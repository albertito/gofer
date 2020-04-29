package proxy

import (
	"context"
	"crypto/tls"
	"errors"
	golog "log"
	"net/http"
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

func httpServer(conf config.HTTP) *http.Server {
	ev := trace.NewEventLog("httpserver", conf.Addr)

	srv := &http.Server{
		Addr: conf.Addr,

		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,

		ErrorLog: golog.New(ev, "", golog.Lshortfile),
	}

	// Load route table.
	mux := http.NewServeMux()
	srv.Handler = mux
	for from, to := range conf.RouteTable {
		toURL, err := url.Parse(to)
		if err != nil {
			log.Fatalf("route %q -> %q: destination is not a valid URL: %v",
				from, to, err)
		}
		log.Infof("%s route %q -> %q", srv.Addr, from, toURL)
		switch toURL.Scheme {
		case "http", "https":
			mux.Handle(from, makeProxy(from, *toURL))
		case "dir":
			mux.Handle(from, makeDir(from, *toURL))
		case "static":
			mux.Handle(from, makeStatic(from, *toURL))
		case "redirect":
			mux.Handle(from, makeRedirect(from, *toURL))
		default:
			log.Fatalf("route %q -> %q: invalid destination scheme %q",
				from, to, toURL.Scheme)
		}
	}

	return srv
}

func HTTP(conf config.HTTP) {
	srv := httpServer(conf)
	lis, err := systemd.Listen("tcp", conf.Addr)
	if err != nil {
		log.Fatalf("%s error listening: %v", conf.Addr, err)
	}
	log.Infof("%s http proxy starting on %q", conf.Addr, lis.Addr())
	err = srv.Serve(lis)
	log.Fatalf("%s http proxy exited: %v", conf.Addr, err)
}

func HTTPS(conf config.HTTPS) {
	var err error
	srv := httpServer(conf.HTTP)

	srv.TLSConfig, err = util.LoadCerts(conf.Certs)
	if err != nil {
		log.Fatalf("%s error loading certs: %v", conf.Addr, err)
	}

	rawLis, err := systemd.Listen("tcp", conf.Addr)
	if err != nil {
		log.Fatalf("%s error listening: %v", conf.Addr, err)
	}

	// We need to set the NextProtos manually before creating the TLS
	// listener, the library cannot help us with this.
	srv.TLSConfig.NextProtos = append(srv.TLSConfig.NextProtos,
		"h2", "http/1.1")
	lis := tls.NewListener(rawLis, srv.TLSConfig)

	log.Infof("%s https proxy starting on %q", conf.Addr, lis.Addr())
	err = srv.Serve(lis)
	log.Fatalf("%s https proxy exited: %v", conf.Addr, err)
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
	if !strings.HasSuffix(a, "/") && !strings.HasPrefix(b, "/") {
		a = a + "/"
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
	//   www.example.com/p/ -> http://dst/q
	//
	// then:
	//   /a/x  goes to  http://dst/b/x (not http://dst/b/a/x)
	//   www.example.com/p/x  goes to  http://dst/q/x
	//
	// It is expected that `from` already has the domain removed using
	// stripDomain.
	dst := joinPath(to, strings.TrimPrefix(req, from))
	if dst == "" || dst[0] != '/' {
		dst = "/" + dst
	}
	return dst
}

func makeDir(from string, to url.URL) http.Handler {
	from = stripDomain(from)

	fs := http.FileServer(http.Dir(to.Path))
	return WithLogging("http:dir",
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tr, _ := trace.FromContext(r.Context())
			tr.Printf("serving dir root %q", to.Path)

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
	return WithLogging("http:static",
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tr, _ := trace.FromContext(r.Context())
			tr.Printf("statically serving %q", to.Path)
			http.ServeFile(w, r, to.Path)
		}),
	)
}

func makeRedirect(from string, to url.URL) http.Handler {
	from = stripDomain(from)

	dst, err := url.Parse(to.Opaque)
	if err != nil {
		log.Fatalf("Invalid destination %q for redirect route: %v",
			to.Opaque, err)
	}

	return WithLogging("http:redirect",
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tr, _ := trace.FromContext(r.Context())
			target := *dst
			target.RawQuery = r.URL.RawQuery
			target.Path = adjustPath(r.URL.Path, from, dst.Path)
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
