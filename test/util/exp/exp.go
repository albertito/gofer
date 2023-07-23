// Fetch an URL, and check if the response matches what we expect.
package main

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var exitCode int = 0

func main() {
	// The first arg is the URL, and then we shift.
	url := os.Args[1]
	os.Args = append([]string{os.Args[0]}, os.Args[2:]...)

	var (
		body = flag.String("body", "",
			"expect body with these exact contents")
		bodyRE = flag.String("bodyre", "",
			"expect body matching these contents (regexp match)")
		bodyNotRE = flag.String("bodynotre", "",
			"expect body NOT matching these contents (regexp match)")
		redir = flag.String("redir", "",
			"expect a redirect to this URL")
		status = flag.Int("status", 200,
			"expect this status code")
		verbose = flag.Bool("v", false,
			"enable verbose output")
		hdrRE = flag.String("hdrre", "",
			"expect a header matching these contents (regexp match)")
		clientErrorRE = flag.String("clienterrorre", "",
			"expect a client error matching these contents (regexp match)")
		caCert = flag.String("cacert", "",
			"file to read CA cert from")
		forceLocalhost = flag.Bool("forcelocalhost", false,
			"force connection to go to localhost")
	)
	flag.Parse()

	client := &http.Client{
		CheckRedirect: noRedirect,
		Transport:     mkTransport(*caCert, *forceLocalhost),
	}

	resp, err := client.Get(url)
	if *clientErrorRE != "" {
		if err == nil {
			errorf("expected client error, got nil")
			os.Exit(exitCode)
		}
		matched, reErr := regexp.MatchString(*clientErrorRE, err.Error())
		if reErr != nil {
			errorf("regexp error: %q\n", reErr)
		}
		if !matched {
			errorf("client error did not match regexp: %q\n", err.Error())
		}
		os.Exit(exitCode)
	} else if err != nil {
		fatalf("error getting %q: %v\n", url, err)
	}
	defer resp.Body.Close()
	rbody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		errorf("error reading body: %v\n", err)
	}

	if *verbose {
		fmt.Printf("Request: %s\n", url)
		fmt.Printf("Response:\n")
		fmt.Printf("  %v  %v\n", resp.Proto, resp.Status)
		ks := []string{}
		for k, _ := range resp.Header {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		for _, k := range ks {
			fmt.Printf("  %v: %s\n", k,
				strings.Join(resp.Header.Values(k), ", "))
		}
		fmt.Printf("\n")
	}

	if resp.StatusCode != *status {
		errorf("status is not %d: %q\n", *status, resp.Status)
	}

	if *body != "" {
		// Unescape the body to allow control characters more easily.
		*body, _ = strconv.Unquote("\"" + *body + "\"")
		if string(rbody) != *body {
			errorf("unexpected body: %q\n", rbody)
		}
	}

	if *bodyRE != "" {
		matched, err := regexp.Match(*bodyRE, rbody)
		if err != nil {
			errorf("regexp error: %q\n", err)
		}
		if !matched {
			errorf("body did not match regexp: %q\n", rbody)
		}
	}

	if *bodyNotRE != "" {
		matched, err := regexp.Match(*bodyNotRE, rbody)
		if err != nil {
			errorf("regexp error: %q\n", err)
		}
		if matched {
			errorf("body matched regexp: %q\n", rbody)
		}
	}

	if *redir != "" {
		if loc := resp.Header.Get("Location"); loc != *redir {
			errorf("unexpected redir location: %q\n", loc)
		}
	}

	if *hdrRE != "" {
		match := false
	outer:
		for k, vs := range resp.Header {
			for _, v := range vs {
				hdr := fmt.Sprintf("%s: %s", k, v)
				matched, err := regexp.MatchString(*hdrRE, hdr)
				if err != nil {
					errorf("regexp error: %q\n", err)
				}
				if matched {
					match = true
					break outer
				}
			}
		}

		if !match {
			errorf("header did not match: %v\n", resp.Header)
		}
	}

	os.Exit(exitCode)
}

func noRedirect(req *http.Request, via []*http.Request) error {
	return http.ErrUseLastResponse
}

func mkTransport(caCert string, forceLocalhost bool) *http.Transport {
	if caCert == "" {
		return nil
	}

	certs, err := ioutil.ReadFile(caCert)
	if err != nil {
		fatalf("error reading CA file %q: %v", caCert, err)
	}

	rootCAs := x509.NewCertPool()
	if ok := rootCAs.AppendCertsFromPEM(certs); !ok {
		fatalf("error adding certs to root")
	}

	t := &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs: rootCAs,
		},
	}

	if forceLocalhost {
		t.Dial = func(network, addr string) (net.Conn, error) {
			_, port, _ := net.SplitHostPort(addr)
			return net.Dial(network, "localhost:"+port)
		}
	}

	return t
}

func fatalf(s string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, s, a...)
	os.Exit(1)
}

func errorf(s string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, s, a...)
	exitCode = 1
}
