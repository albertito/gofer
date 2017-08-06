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

const configTemplate = `
[[http]]
addr = "$FRONTEND_ADDR"
routes = { "/be/" = "$BACKEND_URL" }
`
const backendResponse = "backend response\n"

func TestSimple(t *testing.T) {
	backend := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, backendResponse)
		}))
	defer backend.Close()

	feAddr := getFreePort()

	configStr := strings.NewReplacer(
		"$FRONTEND_ADDR", feAddr,
		"$BACKEND_URL", backend.URL,
	).Replace(configTemplate)

	conf, err := config.LoadString(configStr)
	if err != nil {
		log.Fatal(err)
	}
	t.Logf("conf.HTTP[0]: %#v", *conf.HTTP[0])

	go HTTP(*conf.HTTP[0])

	waitForHTTPServer(feAddr)

	testGet(t, "http://"+feAddr+"/be", 200)
	testGet(t, "http://"+feAddr+"/be/", 200)
	testGet(t, "http://"+feAddr+"/be/2", 200)
	testGet(t, "http://"+feAddr+"/be/3", 200)
	testGet(t, "http://"+feAddr+"/x", 404)
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
