package proxy

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"blitiri.com.ar/go/gofer/config"
	"blitiri.com.ar/go/gofer/util"
)

func httpServer(conf config.HTTP) *http.Server {
	srv := &http.Server{
		Addr:     conf.Addr,
		ErrorLog: util.Log,
		// TODO: timeouts.
	}

	// Load route table.
	mux := http.NewServeMux()
	srv.Handler = mux
	for from, to := range conf.RouteTable {
		toURL, err := url.Parse(to)
		if err != nil {
			util.Log.Fatalf("route %q -> %q: destination is not a valid URL: %v",
				from, to, err)
		}
		util.Log.Printf("%s route %q -> %q", srv.Addr, from, toURL)
		mux.Handle(from, makeProxy(from, *toURL))
	}

	return srv
}

func HTTP(conf config.HTTP) {
	srv := httpServer(conf)
	util.Log.Printf("HTTP proxy on %q", conf.Addr)
	err := srv.ListenAndServe()
	util.Log.Fatalf("HTTP proxy exited: %v", err)
}

func HTTPS(conf config.HTTPS) {
	var err error
	srv := httpServer(conf.HTTP)

	srv.TLSConfig, err = util.LoadCerts(conf.Certs)
	if err != nil {
		util.Log.Fatalf("error loading certs: %v", err)
	}

	util.Log.Printf("HTTPS proxy on %q", srv.Addr)
	err = srv.ListenAndServeTLS("", "")
	util.Log.Fatalf("HTTPS proxy exited: %v", err)
}

func makeProxy(from string, to url.URL) http.Handler {
	proxy := &httputil.ReverseProxy{}
	proxy.ErrorLog = util.Log
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
	if idx := strings.Index(from, "/"); idx > 0 {
		from = from[idx:]
	}

	proxy.Director = func(req *http.Request) {
		req.URL.Scheme = to.Scheme
		req.URL.Host = to.Host
		req.URL.RawQuery = req.URL.RawQuery
		req.URL.Path = joinPath(to.Path, strings.TrimPrefix(req.URL.Path, from))
		if req.URL.Path == "" || req.URL.Path[0] != '/' {
			req.URL.Path = "/" + req.URL.Path
		}

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

	util.Log.Printf("%s %s %s -> %s%s", req.RemoteAddr, req.Proto, req.URL,
		resps, errs)

	return response, err
}

// Use a single logging transport, we don't need more than one.
var transport = &loggingTransport{}
