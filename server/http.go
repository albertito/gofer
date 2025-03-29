package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	golog "log"
	"net"
	"net/http"
	"net/http/cgi"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"blitiri.com.ar/go/gofer/config"
	"blitiri.com.ar/go/gofer/ipratelimit"
	"blitiri.com.ar/go/gofer/ratelimit"
	"blitiri.com.ar/go/gofer/reqlog"
	"blitiri.com.ar/go/gofer/trace"
	"blitiri.com.ar/go/gofer/util"
	"blitiri.com.ar/go/log"
	"blitiri.com.ar/go/systemd"
)

func httpServer(addr string, conf config.HTTP) (*http.Server, error) {
	tr := trace.New("httpserver", addr)
	tr.SetMaxEvents(1000)

	srv := &http.Server{
		Addr: addr,

		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,

		ErrorLog: golog.New(tr, "", golog.Lshortfile),
	}

	mux := http.NewServeMux()
	srv.Handler = mux

	// Load route table.
	for path, r := range conf.Routes {
		if r.Dir != "" {
			log.Infof("%s route %q -> dir %q", srv.Addr, path, r.Dir)
			mux.Handle(path, makeDir(path, r.Dir, r.DirOpts))
		} else if r.File != "" {
			log.Infof("%s route %q -> file %q", srv.Addr, path, r.File)
			mux.Handle(path, makeFile(path, r.File))
		} else if r.Proxy != nil {
			log.Infof("%s route %q -> proxy %s", srv.Addr, path, r.Proxy)
			mux.Handle(path, makeProxy(path, r.Proxy.URL()))
		} else if r.Redirect != nil {
			log.Infof("%s route %q -> redirect %s", srv.Addr, path, r.Redirect)
			mux.Handle(path, makeRedirect(path, r.Redirect.URL()))
		} else if len(r.RedirectRe) > 0 {
			log.Infof("%s route %q -> redirect_re %q",
				srv.Addr, path, r.RedirectRe)
			mux.Handle(path, makeRedirectRe(r.RedirectRe))
		} else if len(r.CGI) > 0 {
			log.Infof("%s route %q -> cgi %q", srv.Addr, path, r.CGI)
			mux.Handle(path, makeCGI(path, r.CGI))
		} else if r.Status > 0 {
			log.Infof("%s route %q -> status %d", srv.Addr, path, r.Status)
			mux.Handle(path, makeStatus(path, r.Status))
		}
	}

	// Wrap the authentication handlers.
	if len(conf.Auth) > 0 {
		authMux := http.NewServeMux()
		for path, dbPath := range conf.Auth {
			users, err := LoadAuthFile(dbPath)
			if err != nil {
				return nil, log.Errorf(
					"failed to load auth file %q: %v", dbPath, err)
			}
			authMux.Handle(path,
				&AuthWrapper{
					handler: srv.Handler,
					users:   users,
				})

			log.Infof("%s auth %q -> %q", srv.Addr, path, dbPath)
		}

		if _, ok := conf.Auth["/"]; !ok {
			authMux.Handle("/", srv.Handler)
		}
		srv.Handler = authMux
	}

	// Extra headers.
	if len(conf.SetHeader) > 0 {
		hdrMux := http.NewServeMux()
		for path, extraHdrs := range conf.SetHeader {
			hdrMux.Handle(path, SetHeader(srv.Handler, extraHdrs))
			log.Infof("%s add headers %q -> %q", srv.Addr, path, extraHdrs)
		}

		if _, ok := conf.SetHeader["/"]; !ok {
			hdrMux.Handle("/", srv.Handler)
		}
		srv.Handler = hdrMux
	}

	// Custom timeouts.
	if len(conf.Timeouts) > 0 {
		timeoutMux := http.NewServeMux()
		for path, timeout := range conf.Timeouts {
			timeoutMux.Handle(path, WithTimeout(srv.Handler, timeout))
			log.Infof("%s timeout %q -> read:%s write:%s",
				srv.Addr, path, timeout.Read, timeout.Write)
		}

		if _, ok := conf.Timeouts["/"]; !ok {
			timeoutMux.Handle("/", srv.Handler)
		}
		srv.Handler = timeoutMux
	}

	// Logging for all entries.
	// Because this will use the request logs if available, it needs to be
	// wrapped by it.
	srv.Handler = WithLogging(srv.Handler)

	if len(conf.ReqLog) > 0 {
		logMux := http.NewServeMux()
		for path, logName := range conf.ReqLog {
			l := reqlog.FromName(logName)
			if l == nil {
				return nil, log.Errorf("unknown reqlog name %q", logName)
			}
			logMux.Handle(path, WithReqLog(srv.Handler, l))
			log.Infof("%s reqlog %q to %q", srv.Addr, path, logName)
		}

		if _, ok := conf.ReqLog["/"]; !ok {
			logMux.Handle("/", srv.Handler)
		}
		srv.Handler = logMux
	}

	// Tracing for all entries.
	srv.Handler = WithTrace("http@"+srv.Addr, srv.Handler)

	// Rate limiting goes outside of tracing, to avoid polluting per-protocol
	// traces with rate-limited events (we trace those separately).
	if len(conf.RateLimit) > 0 {
		rlMux := http.NewServeMux()
		for path, rlName := range conf.RateLimit {
			l := ratelimit.FromName(rlName)
			rlMux.Handle(path, WithRateLimit(srv.Handler, l))
			log.Infof("%s ratelimit %q to %q", srv.Addr, path, rlName)
		}
		if _, ok := conf.RateLimit["/"]; !ok {
			rlMux.Handle("/", srv.Handler)
		}
		srv.Handler = rlMux
	}

	return srv, nil
}

func HTTP(addr string, conf config.HTTP) error {
	srv, err := httpServer(addr, conf)
	if err != nil {
		return err
	}
	lis, err := systemd.Listen("tcp", addr)
	if err != nil {
		return log.Errorf("%s error listening: %v", addr, err)
	}
	log.Infof("%s http starting on %q", addr, lis.Addr())
	err = srv.Serve(lis)
	return log.Errorf("%s http exited: %v", addr, err)
}

func HTTPS(addr string, conf config.HTTPS) error {
	srv, err := httpServer(addr, conf.HTTP)
	if err != nil {
		return err
	}

	srv.TLSConfig, err = util.LoadCertsForHTTPS(conf)
	if err != nil {
		return log.Errorf("%s error loading certs: %v", addr, err)
	}

	rawLis, err := systemd.Listen("tcp", addr)
	if err != nil {
		return log.Errorf("%s error listening: %v", addr, err)
	}

	lis := tls.NewListener(rawLis, srv.TLSConfig)

	log.Infof("%s https starting on %q", addr, lis.Addr())
	err = srv.Serve(lis)
	return log.Errorf("%s https exited: %v", addr, err)
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

func makeDir(path string, dir string, opts config.DirOpts) http.Handler {
	fs := FileServer(NewFS(http.Dir(dir), opts))

	path = stripDomain(path)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tr, _ := trace.FromContext(r.Context())
		tr.Printf("serving dir root %q", dir)

		r.URL.Path = strings.TrimPrefix(r.URL.Path, path)
		if r.URL.Path == "" || r.URL.Path[0] != '/' {
			r.URL.Path = "/" + r.URL.Path
		}
		tr.Printf("adjusted dir: %q", r.URL.Path)
		fs.ServeHTTP(w, r)
	})
}

func makeFile(path string, file string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tr, _ := trace.FromContext(r.Context())
		tr.Printf("serving file %q", file)
		http.ServeFile(w, r, file)
	})
}

func makeCGI(path string, cmd []string) http.Handler {
	path = stripDomain(path)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tr, _ := trace.FromContext(r.Context())
		tr.Printf("exec %q", cmd)
		h := cgi.Handler{
			Path:   cmd[0],
			Args:   cmd[1:],
			Root:   path,
			Logger: golog.New(tr, "", golog.Lshortfile),
			Stderr: tr,
		}
		h.ServeHTTP(w, r)
	})
}

func makeRedirect(path string, to url.URL) http.Handler {
	path = stripDomain(path)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tr, _ := trace.FromContext(r.Context())
		target := to
		target.RawQuery = r.URL.RawQuery
		target.Path = adjustPath(r.URL.Path, path, to.Path)
		tr.Printf("redirect to %q", target.String())

		http.Redirect(w, r, target.String(), http.StatusTemporaryRedirect)
	})
}

func makeRedirectRe(rxs []config.RePair) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tr, _ := trace.FromContext(r.Context())

		for _, rx := range rxs {
			if !rx.From.MatchString(r.URL.Path) {
				continue
			}

			target := rx.From.ReplaceAllString(r.URL.Path, rx.To)
			status := rx.Status
			if status == 0 {
				status = http.StatusTemporaryRedirect
			}

			tr.Printf("matched %q, %d redirect to %q",
				rx.From, status, target)
			http.Redirect(w, r, target, status)
			return
		}

		// No regexp matched, return 404.
		tr.Printf("no regexp matched")
		http.NotFound(w, r)
	})
}

func makeStatus(from string, status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tr, _ := trace.FromContext(r.Context())
		tr.Printf("status %d", status)
		w.WriteHeader(status)
	})
}

func makeProxy(path string, to url.URL) http.Handler {
	proxy := &httputil.ReverseProxy{}
	proxy.ErrorHandler = proxyErrorHandler

	// Rewrite that strips "path" from the request path, so that if we have
	// this config:
	//
	//   /a/ -> http://dst/b
	//   www.example.com/p/ -> http://dst/q
	//
	// then:
	//   /a/x  goes to  http://dst/b/x (not http://dst/b/a/x)
	//   www.example.com/p/x  goes to  http://dst/q/x

	// Strip the domain from `path`, if any. That is useful for the http
	// router, but to us is irrelevant.
	path = stripDomain(path)

	proxy.Rewrite = func(r *httputil.ProxyRequest) {
		// This sets the Forwarded-For, X-Forwarded-Host, and
		// X-Forwarded-Proto headers of the outbound request.
		// The inbound request's X-Forwarded-For header is ignored.
		r.SetXForwarded()

		// Set the Forwarded header, which is a standardized version of the
		// above, and not yet set by SetXForwarded.
		r.Out.Header.Set("Forwarded",
			fmt.Sprintf("for=%q;host=%q;proto=%s",
				r.Out.Header.Get("X-Forwarded-For"),
				r.Out.Header.Get("X-Forwarded-Host"),
				r.Out.Header.Get("X-Forwarded-Proto")))

		// Set the outbound URL based on the target.
		// We use our own path adjustment since the default behaviour doesn't
		// do what we want (see above).
		// Note r.SetURL will merge the two query strings appropriately. It
		// also may strip parts of the query if ';' is used as a separator, as
		// per golang.org/issue/25192. This is fine for us.
		r.SetURL(&to)
		r.Out.URL.Path = adjustPath(r.In.URL.Path, path, to.Path)

		// If the user agent is not set, prevent a fall back to the default
		// value.
		if _, ok := r.Out.Header["User-Agent"]; !ok {
			r.Out.Header.Set("User-Agent", "")
		}

		tr, _ := trace.FromContext(r.In.Context())
		tr.Printf("proxy to: %s %s %s",
			r.Out.Proto, r.Out.Method, r.Out.URL.String())
	}

	return proxy
}

func proxyErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
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
	length int64
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.length += int64(n)
	return n, err
}

// ReadFrom is optional but enables the use of sendfile, which speeds things
// up considerably.
func (w *statusWriter) ReadFrom(src io.Reader) (int64, error) {
	n, err := io.Copy(w.ResponseWriter, src)
	w.length += n
	return n, err
}

// Flush is optional but makes it support the http.Flusher interface, which is
// needed for things like server-side events.
func (w *statusWriter) Flush() {
	flusher, ok := w.ResponseWriter.(http.Flusher)
	if ok {
		flusher.Flush()
	}
}

// Unwrap is used by ResponseController to get the underlying
// http.ResponseWriter.
func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func SetHeader(parent http.Handler, hdrs map[string]string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tr, _ := trace.FromContext(r.Context())
		for k, v := range hdrs {
			w.Header().Set(k, v)
			tr.Printf("added header: %s: %q", k, v)
		}
		parent.ServeHTTP(w, r)
	})
}

func WithTrace(name string, parent http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tr, ok := trace.FromContext(r.Context())
		if !ok {
			tr = trace.New(name, r.Host+r.URL.String())
			defer tr.Finish()

			// Associate the trace with this request.
			r = r.WithContext(trace.NewContext(r.Context(), tr))

			// Log the request on creation.
			tr.Printf("%s %s %s %s %s",
				r.RemoteAddr, r.Proto, r.Method, r.Host, r.URL.String())
		}

		parent.ServeHTTP(w, r)
	})
}

func WithLogging(parent http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tr, _ := trace.FromContext(r.Context())

		// Wrap the writer so we can get output information.
		sw := statusWriter{ResponseWriter: w}

		// Save the URL, since some of the callers will change it (e.g.
		// makeDir).
		origURL := *r.URL

		start := time.Now()
		parent.ServeHTTP(&sw, r)
		lat := time.Since(start)

		tr.Printf("%d %s", sw.status, http.StatusText(sw.status))
		tr.Printf("%d bytes", sw.length)

		if sw.status >= 400 && sw.status != 404 {
			tr.SetError()
		}

		r.URL = &origURL
		reqLog(r, sw.status, sw.length, lat)
	})
}

func WithReqLog(parent http.Handler, rl *reqlog.Log) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Associate the log with this request. Actual logging will be
		// performed within the handlers (see WithLogging).
		r = r.WithContext(reqlog.NewContext(r.Context(), rl))

		parent.ServeHTTP(w, r)
	})
}

func reqLog(r *http.Request, status int, length int64, latency time.Duration) {
	rlog := reqlog.FromContext(r.Context())
	if rlog == nil {
		return
	}
	rlog.Log(&reqlog.Event{
		T:       time.Now(),
		H:       r,
		Status:  status,
		Length:  length,
		Latency: latency,
	})
}

func WithRateLimit(parent http.Handler, rl *ipratelimit.Limiter) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ip net.IP
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ratelimit.Trace(rl).Errorf(
				"[http] failed to split remote address %q: %v",
				r.RemoteAddr, err)
			goto allow
		}

		ip = net.ParseIP(host)
		if ip == nil {
			ratelimit.Trace(rl).Errorf(
				"[http] failed to parse IP address: %q", r.RemoteAddr)
			goto allow
		}

		if !rl.Allow(ip) {
			ratelimit.Trace(rl).Printf(
				"[http] rate limit exceeded for %q", ip)
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}

	allow:
		parent.ServeHTTP(w, r)
	})
}

func WithTimeout(parent http.Handler, timeout config.Timeout) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tr, _ := trace.FromContext(r.Context())

		rc := http.NewResponseController(w)

		if timeout.Read > 0 {
			tr.Printf("read timeout: %v", timeout.Read)
			rc.SetReadDeadline(time.Now().Add(timeout.Read))
		}

		if timeout.Write > 0 {
			tr.Printf("write timeout: %v", timeout.Write)
			rc.SetWriteDeadline(time.Now().Add(timeout.Write))
		}

		parent.ServeHTTP(w, r)
	})
}
