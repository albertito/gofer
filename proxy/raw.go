package proxy

import (
	"crypto/tls"
	"fmt"
	"net"

	"blitiri.com.ar/go/gofer/config"
	"blitiri.com.ar/go/gofer/trace"
	"blitiri.com.ar/go/gofer/util"
	"blitiri.com.ar/go/log"
	"blitiri.com.ar/go/systemd"
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
		lis, err = systemd.Listen("tcp", conf.Addr)
		lis = tls.NewListener(lis, tlsConfig)
	} else {
		lis, err = systemd.Listen("tcp", conf.Addr)
	}
	if err != nil {
		log.Fatalf("Raw proxy error listening on %q: %v", conf.Addr, err)
	}

	log.Infof("Raw proxy on %q (%q)", conf.Addr, lis.Addr())
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

	util.BidirCopy(src, dst)

	tr.Printf("copy complete")
}
