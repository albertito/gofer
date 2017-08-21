package proxy

import (
	"crypto/tls"
	"net"
	"time"

	"blitiri.com.ar/go/gofer/config"
	"blitiri.com.ar/go/gofer/util"
	"blitiri.com.ar/go/log"
)

func Raw(conf config.Raw) {
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
		lis, err = tls.Listen("tcp", conf.Addr, tlsConfig)
	} else {
		lis, err = net.Listen("tcp", conf.Addr)
	}
	if err != nil {
		log.Fatalf("error listening: %v", err)
	}

	log.Infof("Raw proxy on %q", conf.Addr)
	for {
		conn, err := lis.Accept()
		if err != nil {
			log.Fatalf("%s error accepting: %v", conf.Addr, err)
		}

		go forward(conn, conf.To, conf.ToTLS)
	}
}

func forward(src net.Conn, dstAddr string, dstTLS bool) {
	defer src.Close()

	start := time.Now()
	var dst net.Conn
	var err error
	if dstTLS {
		dst, err = tls.Dial("tcp", dstAddr, nil)
	} else {
		dst, err = net.Dial("tcp", dstAddr)
	}

	if err != nil {
		log.Errorf("%s error dialing back: %v", src.LocalAddr(), err)
		return
	}
	defer dst.Close()

	startCopy := time.Now()
	util.BidirCopy(src, dst)
	end := time.Now()

	log.Infof("%s raw %s -> %s (%s+%s=%s)",
		src.RemoteAddr(), src.LocalAddr(), dst.RemoteAddr(),
		startCopy.Sub(start), end.Sub(startCopy), end.Sub(start))
}
