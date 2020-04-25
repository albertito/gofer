package proxy

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"blitiri.com.ar/go/gofer/config"
	"blitiri.com.ar/go/gofer/util"
	"blitiri.com.ar/go/log"
	"blitiri.com.ar/go/systemd"
)

func httpServer(conf config.HTTP) *http.Server {
	srv := &http.Server{
		Addr: conf.Addr,
		// TODO: timeouts.
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
		log.Fatalf("HTTP proxy error listening on %q: %v", conf.Addr, err)
	}
	log.Infof("HTTP proxy on %q (%q)", conf.Addr, lis.Addr())
	err = srv.Serve(lis)
	log.Fatalf("HTTP proxy exited: %v", err)
}

func HTTPS(conf config.HTTPS) {
	var err error
	srv := httpServer(conf.HTTP)

	srv.TLSConfig, err = util.LoadCerts(conf.Certs)
	if err != nil {
		log.Fatalf("error loading certs: %v", err)
	}

	rawLis, err := systemd.Listen("tcp", conf.Addr)
	if err != nil {
		log.Fatalf("HTTPS proxy error listening on %q: %v", conf.Addr, err)
	}

	// We need to set the NextProtos manually before creating the TLS
	// listener, the library cannot help us with this.
	srv.TLSConfig.NextProtos = append(srv.TLSConfig.NextProtos,
		"h2", "http/1.1")
	lis := tls.NewListener(rawLis, srv.TLSConfig)

	log.Infof("HTTPS proxy on %q (%q)", conf.Addr, lis.Addr())
	err = srv.Serve(lis)
	log.Fatalf("HTTPS proxy exited: %v", err)
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

	return proxy
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
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = strings.TrimPrefix(r.URL.Path, from)
		if r.URL.Path == "" || r.URL.Path[0] != '/' {
			r.URL.Path = "/" + r.URL.Path
		}
		fs.ServeHTTP(w, r)
	})
}

func makeStatic(from string, to url.URL) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, to.Path)
	})
}

func makeRedirect(from string, to url.URL) http.Handler {
	from = stripDomain(from)

	dst, err := url.Parse(to.Opaque)
	if err != nil {
		log.Fatalf("Invalid destination %q for redirect route: %v",
			to.Opaque, err)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := *dst
		target.RawQuery = r.URL.RawQuery
		target.Path = adjustPath(r.URL.Path, from, dst.Path)

		http.Redirect(w, r, target.String(), http.StatusTemporaryRedirect)
	})
}

type loggingTransport struct{}

func (t *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	response, err := http.DefaultTransport.RoundTrip(req)

	errs := ""
	if err != nil {
		errs = " (" + err.Error() + ")"
	}

	resps := "<nil>"
	if response != nil {
		resps = fmt.Sprintf("%d", response.StatusCode)
	}

	// 1.2.3.4:34575 HTTP/2.0 domain.com https://backend/path -> 200
	log.Infof("%s %s %s %s -> %s%s",
		req.RemoteAddr, req.Proto, req.Host, req.URL,
		resps, errs)

	return response, err
}

// Use a single logging transport, we don't need more than one.
var transport = &loggingTransport{}
