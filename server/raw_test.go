package server

import (
	"net"
	"testing"
	"time"

	"blitiri.com.ar/go/gofer/config"
	"blitiri.com.ar/go/gofer/ratelimit"
)

func TestAllowedOnNonTCP(t *testing.T) {
	// Use a rate limit with 0 requests per second to disable ratelimiting.
	ratelimit.FromConfig("test-rl", config.RateLimit{
		Rate: config.Rate{Requests: 0, Period: time.Second}})
	rl := ratelimit.FromName("test-rl")

	tcp := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}
	if allowed(tcp, rl) {
		t.Errorf("allowed(tcp %v) = true, expected false", tcp)
	}

	// Try a few different non-TCP addresses, to make sure we fail-open on
	// them.
	addrs := []net.Addr{
		&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234},
		&net.IPAddr{IP: net.IPv4(127, 0, 0, 1)},
	}
	for _, addr := range addrs {
		if !allowed(addr, rl) {
			t.Errorf("allowed(%v) = false, expected true", addr)
		}
	}
}
