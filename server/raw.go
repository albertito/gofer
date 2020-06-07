package server

import (
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"blitiri.com.ar/go/gofer/config"
	"blitiri.com.ar/go/gofer/reqlog"
	"blitiri.com.ar/go/gofer/trace"
	"blitiri.com.ar/go/gofer/util"
	"blitiri.com.ar/go/log"
	"blitiri.com.ar/go/systemd"
)

func Raw(addr string, conf config.Raw) {
	var err error

	var tlsConfig *tls.Config
	if conf.Certs != "" {
		tlsConfig, err = util.LoadCerts(conf.Certs)
		if err != nil {
			log.Fatalf("error loading certs: %v", err)
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
		log.Fatalf("Raw proxy error listening on %q: %v", addr, err)
	}

	rlog := reqlog.FromName(conf.ReqLog)

	log.Infof("Raw proxy on %q (%q)", addr, lis.Addr())
	for {
		conn, err := lis.Accept()
		if err != nil {
			log.Fatalf("%s error accepting: %v", addr, err)
		}

		go forward(conn, conf.To, conf.ToTLS, rlog)
	}
}

func forward(src net.Conn, dstAddr string, dstTLS bool, rlog *reqlog.Log) {
	defer src.Close()
	start := time.Now()

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
