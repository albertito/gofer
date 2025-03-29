package server

import (
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"blitiri.com.ar/go/gofer/config"
	"blitiri.com.ar/go/gofer/ratelimit"
	"blitiri.com.ar/go/log"
)

// WaitForHTTPServer waits 5 seconds for an HTTP server to start, and returns
// an error if it fails to do so.
// It does this by repeatedly querying the server until it either replies or
// times out.
func waitForHTTPServer(addr string) error {
	c := http.Client{
		Timeout: 100 * time.Millisecond,
	}

	deadline := time.Now().Add(5 * time.Second)
	tick := time.Tick(100 * time.Millisecond)

	for (<-tick).Before(deadline) {
		_, err := c.Get("http://" + addr + "/testpoke")
		if err == nil {
			return nil
		}
	}

	return fmt.Errorf("timed out")
}

// Get a free (TCP) port. This is hacky and not race-free, but it works well
// enough for testing purposes.
func getFreePort() string {
	l, _ := net.Listen("tcp", "localhost:0")
	defer l.Close()
	return l.Addr().String()
}

const backendResponse = "backend response\n"

// Addresses of the proxy under test (created by TestMain).
var (
	httpAddr string
	rawAddr  string
)

// startServer for testing. Returns raw addr, http addr, and the backend test
// server (which should be closed afterwards).
// Note it leaks goroutines, we're ok with this for testing.
func TestMain(m *testing.M) {
	backend := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, backendResponse)
		}))
	defer backend.Close()

	// We have two frontends: one raw and one http.
	rawAddr = getFreePort()
	httpAddr = getFreePort()

	log.Default.Level = log.Error

	pwd, _ := os.Getwd()

	const configTemplate = `
raw:
  "$RAW_ADDR":
    to: "$BACKEND_ADDR"

http:
  "$HTTP_ADDR":
    routes:
      "/be/": { proxy: "$BACKEND_URL" }
      "localhost/xy/": { proxy: "$BACKEND_URL" }
      "/static/hola": { file: "$PWD/testdata/hola" }
      "/dir/": { dir: "$PWD/testdata/" }
      "/redir/": { redirect: "http://$HTTP_ADDR/dir/" }
`
	configStr := strings.NewReplacer(
		"$RAW_ADDR", rawAddr,
		"$HTTP_ADDR", httpAddr,
		"$BACKEND_URL", backend.URL,
		"$BACKEND_ADDR", backend.Listener.Addr().String(),
		"$PWD", pwd,
	).Replace(configTemplate)

	conf, err := config.LoadString(configStr)
	if err != nil {
		log.Fatalf("error loading test config: %v", err)
	}

	go Raw(rawAddr, conf.Raw[rawAddr])
	go HTTP(httpAddr, conf.HTTP[httpAddr])

	waitForHTTPServer(httpAddr)
	waitForHTTPServer(rawAddr)

	os.Exit(m.Run())
}

func TestSimple(t *testing.T) {
	// Test the raw proxy.
	testGet(t, "http://"+rawAddr+"/be", 200)

	// Test the HTTP proxy. Try a combination of URLs and error responses just
	// to exercise a bit more of the path handling and error checking code.
	testGet(t, "http://"+httpAddr+"/be", 200)
	testGet(t, "http://"+httpAddr+"/be/", 200)
	testGet(t, "http://"+httpAddr+"/be/2", 200)
	testGet(t, "http://"+httpAddr+"/be/3", 200)
	testGet(t, "http://"+httpAddr+"/x", 404)
	testGet(t, "http://"+httpAddr+"/xy/1", 404)

	// Test the domain-based routing.
	_, httpPort, _ := net.SplitHostPort(httpAddr)
	testGet(t, "http://localhost:"+httpPort+"/be/", 200)
	testGet(t, "http://localhost:"+httpPort+"/xy/1", 200)

	// Test dir and file schemes.
	testGet(t, "http://"+httpAddr+"/static/hola", 200)
	testGet(t, "http://"+httpAddr+"/dir/hola", 200)
	testGet(t, "http://"+httpAddr+"/redir/hola", 200)
}

func testGet(t *testing.T, url string, expectedStatus int) {
	t.Helper()
	t.Logf("URL: %s", url)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("status %v", resp.Status)

	if resp.StatusCode != expectedStatus {
		t.Errorf("expected status %d, got %v", expectedStatus, resp.Status)
		t.Errorf("response: %#v", resp)
	}

	// We don't care about the body for non-200 responses.
	if resp.StatusCode != http.StatusOK {
		return
	}

	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != backendResponse {
		t.Errorf("expected body = %q, got %q", backendResponse, string(b))
	}

	t.Logf("response body: %q", b)
}

func TestJoinPath(t *testing.T) {
	cases := []struct{ a, b, expected string }{
		{"/a/", "", "/a/"},
		{"/a/", "b", "/a/b"},
		{"/a/", "b/", "/a/b/"},
		{"a/", "", "a/"},
		{"a/", "b", "a/b"},
		{"a/", "b/", "a/b/"},
		{"a/", "/b/", "a/b/"},
		{"/", "", "/"},
		{"", "", "/"},
		{"/", "/", "/"},
	}
	for _, c := range cases {
		got := joinPath(c.a, c.b)
		if got != c.expected {
			t.Errorf("join %q, %q = %q, expected %q", c.a, c.b, got, c.expected)
		}
	}
}

func TestAdjustPath(t *testing.T) {
	cases := []struct{ from, to, req, expected string }{
		{"/", "/", "/", "/"},
		{"/", "/", "/a", "/a"},
		{"/", "/", "/a/x", "/a/x"},
		{"/a", "/", "/a", "/"},
		{"/a", "/", "/a/", "/"},
		{"/a", "/", "/a/x", "/x"},
		{"/a/", "/", "/a/", "/"},
		{"/a/", "/", "/a/x", "/x"},
		{"/a/", "/b", "/a/", "/b"},
		{"/a/", "/b", "/a/x", "/b/x"},
		{"/p/q", "/r/s", "/p/q", "/r/s"},
		{"/p/q", "/r/s", "/p/q", "/r/s"},
		{"/p/q", "/r/s", "/p/q/x", "/r/s/x"},
	}
	for _, c := range cases {
		got := adjustPath(c.req, c.from, c.to)
		if got != c.expected {
			t.Errorf("adjustPath(%q, %q, %q) = %q, expected %q",
				c.req, c.from, c.to, got, c.expected)
		}
	}
}

func TestAdjustPathPanic(t *testing.T) {
	// We expect req to have from as prefix; otherwise, the function should
	// panic.
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("adjustPath did not panic as expected")
		}
	}()
	adjustPath("/req", "/from", "/to")
}

func TestRateLimitRequestErrors(t *testing.T) {
	// WithRateLimit needs to split host and port, and parse the host IP
	// address. We don't expect either to fail, due to the nature of the
	// handlers, but we want to make sure we fail-open if they do.
	// To test this, we set up some artificial requests.

	// First, set up a rate limiter which rejects everything.
	ratelimit.FromConfig("TestRateLimitRequestErrors", config.RateLimit{
		Rate: config.Rate{Requests: 0, Period: 1 * time.Second}})
	rl := ratelimit.FromName("TestRateLimitRequestErrors")
	if rl == nil {
		t.Fatalf("rate limiter not found")
	}

	// Set up a trivial root handler, and then wrap it with the rate limiter.
	rootHCalled := 0
	rootH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rootHCalled++
		w.WriteHeader(200)
	})

	h := WithRateLimit(rootH, rl)

	// Test that a well-formed request is rejected.
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "http://localhost:1234/", nil)
	h.ServeHTTP(w, r)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected status 429 (too many requests), got %v", w.Code)
	}

	// Test that a request with a remote address that fails to be split
	// fails-open and is allowed.
	r = httptest.NewRequest("GET", "http://[::1]:1234/", nil)
	r.RemoteAddr = "invalid"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 (OK), got %v", w.Code)
	}
	if rootHCalled != 1 {
		t.Errorf("expected root handler to be called 1, got %v", rootHCalled)
	}

	// Test that a request with a remote address that has a non-IP as host
	// fails-open and is allowed.
	r = httptest.NewRequest("GET", "http://[::1]:1234/", nil)
	r.RemoteAddr = "localhost:1234"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 (OK), got %v", w.Code)
	}
	if rootHCalled != 2 {
		t.Errorf("expected root handler to be called 2, got %v", rootHCalled)
	}
}

func Benchmark(b *testing.B) {
	makeBench := func(url string) func(b *testing.B) {
		return func(b *testing.B) {
			var resp *http.Response
			var err error
			for i := 0; i < b.N; i++ {
				resp, err = http.Get(url)
				if err != nil {
					b.Fatal(err)
				}
				resp.Body.Close()
				if resp.StatusCode != 200 {
					b.Errorf("expected status 200, got %v", resp.Status)
					b.Fatalf("response: %#v", resp)
				}
			}
		}
	}

	b.Run("HTTP", makeBench("http://"+httpAddr+"/be"))
	b.Run("Raw", makeBench("http://"+rawAddr+"/be"))
}

func BenchmarkParallel(b *testing.B) {
	makeP := func(url string) func(pb *testing.PB) {
		return func(pb *testing.PB) {
			var resp *http.Response
			var err error
			for pb.Next() {
				resp, err = http.Get(url)
				if err != nil {
					b.Fatal(err)
				}
				resp.Body.Close()
				if resp.StatusCode != 200 {
					b.Errorf("expected status 200, got %v", resp.Status)
					b.Fatalf("response: %#v", resp)
				}
			}
		}
	}

	b.Run("HTTP", func(b *testing.B) {
		b.RunParallel(makeP("http://" + httpAddr + "/be"))
	})
	b.Run("Raw", func(b *testing.B) {
		b.RunParallel(makeP("http://" + rawAddr + "/be"))
	})
}
