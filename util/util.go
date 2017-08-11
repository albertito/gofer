// Package util implements some common utilities.
package util

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"log/syslog"
	"os"
	"path/filepath"
)

// Log is used to log messages.
var Log *log.Logger

func init() {
	// Always have a log from early initialization; this helps with coding
	// errors and can simplify some tests.
	Log = log.New(os.Stderr, "<early> ",
		log.Ldate|log.Ltime|log.Lmicroseconds|log.Lshortfile)
}

// Flags.
var (
	logfile = flag.String("logfile", "-",
		"File to write logs to, use '-' for stdout")
)

func InitLog() {
	var err error
	var logfd io.Writer
	var flags int

	if *logfile == "-" {
		logfd = os.Stdout
		flags |= log.Lshortfile
	} else if *logfile != "" {
		logfd, err = os.OpenFile(*logfile,
			os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0664)
		if err != nil {
			log.Fatalf("error opening log file %s: %v", *logfile, err)
		}
		flags |= log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile
	} else {
		logfd, err = syslog.New(
			syslog.LOG_INFO|syslog.LOG_DAEMON, "gofer")
		if err != nil {
			log.Fatalf("error opening syslog: %v", err)
		}
		flags |= log.Lshortfile
	}

	Log = log.New(logfd, "", flags)
}

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

func BidirCopy(src, dst io.ReadWriter) {
	done := make(chan bool, 2)

	go func() {
		io.Copy(src, dst)
		done <- true
	}()

	go func() {
		io.Copy(dst, src)
		done <- true
	}()

	// Return when one of the two completes.
	// The other goroutine will remain alive, it is up to the caller to create
	// the conditions to complete it (e.g. by closing one of the sides).
	<-done
}
