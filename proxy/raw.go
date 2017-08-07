package proxy

import (
	"crypto/tls"
	"net"

	"blitiri.com.ar/go/gofer/config"
	"blitiri.com.ar/go/gofer/util"
)

func Raw(conf config.Raw) {
	var err error

	var tlsConfig *tls.Config
	if conf.Certs != "" {
		tlsConfig, err = util.LoadCerts(conf.Certs)
		if err != nil {
			util.Log.Fatalf("error loading certs: %v", err)
		}
	}

	var lis net.Listener
	if tlsConfig != nil {
		lis, err = tls.Listen("tcp", conf.Addr, tlsConfig)
	} else {
		lis, err = net.Listen("tcp", conf.Addr)
	}
	if err != nil {
		util.Log.Fatalf("error listening: %v", err)
	}

	util.Log.Printf("Raw proxy on %q", conf.Addr)
	for {
		conn, err := lis.Accept()
		if err != nil {
			util.Log.Fatalf("%s error accepting: %v", conf.Addr, err)
		}

		go forward(conn, conf.To, conf.ToTLS)
	}
}

func forward(src net.Conn, dstAddr string, dstTLS bool) {
	defer src.Close()

	var dst net.Conn
	var err error
	if dstTLS {
		dst, err = tls.Dial("tcp", dstAddr, nil)
	} else {
		dst, err = net.Dial("tcp", dstAddr)
	}

	if err != nil {
		util.Log.Printf("%s error dialing back: %v", src.LocalAddr(), err)
		return
	}
	defer dst.Close()

	util.Log.Printf("%s raw %s -> %s: open",
		src.RemoteAddr(), src.LocalAddr(), dst.RemoteAddr())

	util.BidirCopy(src, dst)

	util.Log.Printf("%s raw %s -> %s: close",
		src.RemoteAddr(), src.LocalAddr(), dst.RemoteAddr())
}
