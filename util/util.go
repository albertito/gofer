// Package util implements some common utilities.
package util

import (
	"crypto/tls"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync/atomic"
)

// LoadCerts loads certificates from the given directory, and returns a TLS
// config including them.
func LoadCerts(certDir string) (*tls.Config, error) {
	tlsConfig := &tls.Config{}

	infos, err := ioutil.ReadDir(certDir)
	if err != nil {
		return nil, fmt.Errorf("ReadDir(%q): %v", certDir, err)
	}
	for _, info := range infos {
		name := info.Name()
		dir := filepath.Join(certDir, name)
		if fi, err := os.Stat(dir); err == nil && !fi.IsDir() {
			// Skip non-directories.
			continue
		}

		certPath := filepath.Join(dir, "fullchain.pem")
		if _, err := os.Stat(certPath); os.IsNotExist(err) {
			continue
		}
		keyPath := filepath.Join(dir, "privkey.pem")
		if _, err := os.Stat(keyPath); os.IsNotExist(err) {
			continue
		}

		cert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			return nil, fmt.Errorf("error loading pair (%q, %q): %v",
				certPath, keyPath, err)
		}
		tlsConfig.Certificates = append(tlsConfig.Certificates, cert)
	}

	if len(tlsConfig.Certificates) == 0 {
		return nil, fmt.Errorf("no certificates found in %q", certDir)
	}

	tlsConfig.BuildNameToCertificate()

	return tlsConfig, nil
}

func BidirCopy(src, dst io.ReadWriter) int64 {
	done := make(chan bool, 2)
	var total int64

	go func() {
		n, _ := io.Copy(src, dst)
		atomic.AddInt64(&total, n)
		done <- true
	}()

	go func() {
		n, _ := io.Copy(dst, src)
		atomic.AddInt64(&total, n)
		done <- true
	}()

	// Return when one of the two completes.
	// The other goroutine will remain alive, it is up to the caller to create
	// the conditions to complete it (e.g. by closing one of the sides).
	<-done

	return atomic.LoadInt64(&total)
}
