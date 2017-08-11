package proxy

import (
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"blitiri.com.ar/go/gofer/config"
)

const backendResponse = "backend response\n"

func TestSimple(t *testing.T) {
	backend := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, backendResponse)
		}))
	defer backend.Close()

	// We have two frontends: one raw and one http.
	rawAddr := getFreePort()
	httpAddr := getFreePort()

	const configTemplate = `
[[raw]]
addr = "$RAW_ADDR"
to = "$BACKEND_ADDR"

[[http]]
addr = "$HTTP_ADDR"

[http.routes]
"/be/" = "$BACKEND_URL"
"localhost/xy/" = "$BACKEND_URL"
`
	configStr := strings.NewReplacer(
		"$RAW_ADDR", rawAddr,
		"$HTTP_ADDR", httpAddr,
		"$BACKEND_URL", backend.URL,
		"$BACKEND_ADDR", backend.Listener.Addr().String(),
	).Replace(configTemplate)

	conf, err := config.LoadString(configStr)
	if err != nil {
		log.Fatal(err)
	}
	t.Logf("conf.Raw[0]: %#v", conf.Raw[0])
	t.Logf("conf.HTTP[0]: %#v", *conf.HTTP[0])

	go Raw(conf.Raw[0])
	go HTTP(*conf.HTTP[0])

	waitForHTTPServer(httpAddr)
	waitForHTTPServer(rawAddr)

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
}

func testGet(t *testing.T, url string, expectedStatus int) {
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

func TestJoinPath(t *testing.T) {
	cases := []struct{ a, b, expected string }{
		{"/a/", "", "/a/"},
		{"/a/", "b", "/a/b"},
		{"/a/", "b/", "/a/b/"},
		{"a/", "", "a/"},
		{"a/", "b", "a/b"},
		{"a/", "b/", "a/b/"},
		{"/", "", "/"},
		{"", "", "/"},
		{"/", "/", "//"}, // Not sure if we want this, but ok for now.
	}
	for _, c := range cases {
		got := joinPath(c.a, c.b)
		if got != c.expected {
			t.Errorf("join %q, %q = %q, expected %q", c.a, c.b, got, c.expected)
		}
	}
}
