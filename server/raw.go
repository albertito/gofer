package server

import (
	"crypto/tls"
	"fmt"
	"net"
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

func Raw(addr string, conf config.Raw) error {
	var err error

	var tlsConfig *tls.Config
	if conf.Certs != "" {
		tlsConfig, err = util.LoadCertsFromDir(conf.Certs)
		if err != nil {
			return log.Errorf("error loading certs: %v", err)
		}
	}

	var lis net.Listener
	if tlsConfig != nil {
		lis, err = systemd.Listen("tcp", addr)
		lis = tls.NewListener(lis, tlsConfig)
	} else {
		lis, err = systemd.Listen("tcp", addr)
	}
	if err != nil {
		return log.Errorf("Raw proxy error listening on %q: %v", addr, err)
	}

	rlog := reqlog.FromName(conf.ReqLog)
	lim := ratelimit.FromName(conf.RateLimit)

	log.Infof("%s raw proxy starting on %q", addr, lis.Addr())
	for {
		conn, err := lis.Accept()
		if err != nil {
			return log.Errorf("%s error accepting: %v", addr, err)
		}

		go forward(conn, conf.To, conf.ToTLS, rlog, lim)
	}
}

func allowed(addr net.Addr, lim *ipratelimit.Limiter) bool {
	// We only support raw proxying over TCP, so we can assume the address is
	// a TCP address. If not, fail-open just to be safe.
	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok {
		ratelimit.Trace(lim).Errorf(
			"[raw] non-TCP address %q", addr)
		return true
	}

	if !lim.Allow(tcpAddr.IP) {
		ratelimit.Trace(lim).Printf(
			"[raw] rate limit exceeded for %q", tcpAddr.IP)
		return false
	}

	return true
}

func forward(src net.Conn, dstAddr string, dstTLS bool,
	rlog *reqlog.Log, lim *ipratelimit.Limiter) {
	defer src.Close()
	start := time.Now()

	if lim != nil && !allowed(src.RemoteAddr(), lim) {
		return
	}

	tr := trace.New("raw", fmt.Sprintf("%s -> %s", src.LocalAddr(), dstAddr))
	defer tr.Finish()

	tr.Printf("remote: %s ", src.RemoteAddr())
	tr.Printf("%s -> %s (tls=%v)",
		src.LocalAddr(), dstAddr, dstTLS)

	var dst net.Conn
	var err error
	if dstTLS {
		dst, err = tls.Dial("tcp", dstAddr, nil)
	} else {
		dst, err = net.Dial("tcp", dstAddr)
	}

	if err != nil {
		tr.Errorf("%s error dialing %v : %v", src.LocalAddr(), dstAddr, err)
		if rlog != nil {
			rlog.Log(&reqlog.Event{
				T: time.Now(),
				R: &reqlog.RawRequest{
					RemoteAddr: src.RemoteAddr(),
					LocalAddr:  src.LocalAddr(),
				},
				Status:  500,
				Latency: time.Since(start),
			})
		}
		return
	}
	defer dst.Close()

	tr.Printf("dial complete: %v -> %v", dst.LocalAddr(), dst.RemoteAddr())

	nbytes := util.BidirCopy(src, dst)
	latency := time.Since(start)

	tr.Printf("copy complete")
	if rlog != nil {
		rlog.Log(&reqlog.Event{
			T: time.Now(),
			R: &reqlog.RawRequest{
				RemoteAddr: src.RemoteAddr(),
				LocalAddr:  src.LocalAddr(),
			},
			Status:  200,
			Length:  nbytes,
			Latency: latency,
		})
	}
}
