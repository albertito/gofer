// ACME (RFC 8555) server, for testing purposes only.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	addr = flag.String("addr", "", "address to listen on")

	caCertFile = flag.String("cacert_file", ".acmesrv.cert",
		"file to write the CA certificate to")
)

type Server struct {
	lis net.Listener

	caKey  *ecdsa.PrivateKey
	caCert []byte
	caTmpl *x509.Certificate

	orderID int

	// Order ID -> Certificate.
	orderCert map[int][]byte
}

func NewServer(addr string) (*Server, error) {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	s := &Server{
		lis:       lis,
		orderCert: map[int][]byte{},

		// Start with a high order ID to make debugging easier.
		orderID: 2000,
	}

	// Generate root.
	s.caKey, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	s.caTmpl = &x509.Certificate{
		IsCA:         true,
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{
			Organization: []string{"acmesrv"},
			CommonName:   "acmesrv CA",
		},
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,

		// Make this live longer than the leaf certs.
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(365 * 24 * time.Hour),
	}

	s.caCert, err = x509.CreateCertificate(
		rand.Reader, s.caTmpl, s.caTmpl, &(s.caKey.PublicKey), s.caKey)
	if err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Server) url() string {
	return "http://" + s.lis.Addr().String()
}

func (s *Server) orderurl(path string, id int) string {
	return fmt.Sprintf("%s/%s/%d", s.url(), path, id)
}

// Get an order ID from the request's URL.
func getOID(r *http.Request) int {
	// Example: http://blah/order/1234
	id, err := strconv.Atoi(strings.Split(r.URL.Path, "/")[2])
	if err != nil {
		panic(err)
	}
	return id
}

func (s *Server) directory(w http.ResponseWriter, r *http.Request) {
	// https://www.rfc-editor.org/rfc/rfc8555.html#section-7.1.1
	url := s.url()
	resp := &struct {
		NewNonce   string `json:"newNonce"`
		NewAccount string `json:"newAccount"`
		NewOrder   string `json:"newOrder"`
		NewAuthz   string `json:"newAuthz"`
	}{
		NewNonce:   url + "/new-nonce",
		NewAccount: url + "/new-acct",
		NewOrder:   url + "/new-order",
		NewAuthz:   url + "/new-authz", // Not needed.
	}
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) newNonce(w http.ResponseWriter, r *http.Request) {
	// https://www.rfc-editor.org/rfc/rfc8555#section-7.2
	w.Header().Set("Replay-Nonce", "test-nonce")
}

func (s *Server) newAccount(w http.ResponseWriter, r *http.Request) {
	logPayload(r)

	// https://www.rfc-editor.org/rfc/rfc8555.html#section-7.3
	w.Header().Set("Replay-Nonce", "test-nonce")
	w.Header().Set("Location", s.url()+"/acct/a1111")
	w.WriteHeader(http.StatusCreated)
	w.Write([]byte("{}"))

}

func (s *Server) newOrder(w http.ResponseWriter, r *http.Request) {
	// https://www.rfc-editor.org/rfc/rfc8555#section-7.4
	logPayload(r)

	oid := s.orderID
	s.orderID++

	w.Header().Set("Replay-Nonce", "test-nonce")
	w.Header().Set("Location", s.orderurl("orders", oid))
	w.WriteHeader(http.StatusCreated)

	resp := struct {
		Status   string   `json:"status"`
		Auths    []string `json:"authorizations"`
		Finalize string   `json:"finalize"`
	}{
		Status:   "pending",
		Auths:    append([]string{}, s.orderurl("auth", oid)),
		Finalize: s.orderurl("finalize", oid),
	}
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) auth(w http.ResponseWriter, r *http.Request) {
	// https://www.rfc-editor.org/rfc/rfc8555#section-7.5
	logPayload(r)

	w.Header().Set("Replay-Nonce", "test-nonce")

	resp := struct {
		Status string `json:"status"`
	}{
		Status: "valid",
	}
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) orders(w http.ResponseWriter, r *http.Request) {
	logPayload(r)

	oid := getOID(r)
	w.Header().Set("Replay-Nonce", "test-nonce")
	resp := struct {
		Status   string `json:"status"`
		Finalize string `json:"finalize"`
		Cert     string `json:"certificate"`
	}{
		Status:   "valid",
		Finalize: s.orderurl("finalize", oid),
		Cert:     s.orderurl("cert", oid),
	}
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) finalize(w http.ResponseWriter, r *http.Request) {
	// https://www.rfc-editor.org/rfc/rfc8555#section-7.4
	oid := getOID(r)
	req := struct {
		CSR string
	}{}
	decodePayload(r, &req)
	b, _ := base64.RawURLEncoding.DecodeString(req.CSR)
	csr, err := x509.ParseCertificateRequest(b)
	if err != nil {
		panic(err)
	}
	fmt.Printf("  csr for %v\n", csr.DNSNames)
	s.generateCert(oid, csr)

	w.Header().Set("Replay-Nonce", "test-nonce")
	resp := struct {
		Status   string `json:"status"`
		Finalize string `json:"finalize"`
		Cert     string `json:"certificate"`
	}{
		Status:   "valid",
		Finalize: s.orderurl("finalize", oid),
		Cert:     s.orderurl("cert", oid),
	}
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) generateCert(oid int, csr *x509.CertificateRequest) {
	leaf := &x509.Certificate{
		SerialNumber: big.NewInt(int64(oid)),
		Subject:      pkix.Name{Organization: []string{"acmesrv"}},
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     csr.DNSNames,

		// Make the certificate long-lived, otherwise we may hit autocert's
		// renewal window, and cause it to continuously renew the certificates.
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(90 * 24 * time.Hour),

		BasicConstraintsValid: true,
	}

	cert, err := x509.CreateCertificate(
		rand.Reader, leaf, s.caTmpl, csr.PublicKey, s.caKey)
	if err != nil {
		panic(err)
	}
	s.orderCert[oid] = cert
}

func (s *Server) cert(w http.ResponseWriter, r *http.Request) {
	// https://www.rfc-editor.org/rfc/rfc8555#section-7.4.2
	logPayload(r)
	oid := getOID(r)
	w.Header().Set("Replay-Nonce", "test-nonce")
	w.Header().Set("Content-Type", "application/pem-certificate-chain")
	pem.Encode(w, &pem.Block{Type: "CERTIFICATE", Bytes: s.orderCert[oid]})
	pem.Encode(w, &pem.Block{Type: "CERTIFICATE", Bytes: s.caCert})
}

func (s *Server) writeCACert() {
	f, err := os.Create(*caCertFile)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: s.caCert})
}

func (s *Server) newAuthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Replay-Nonce", "test-nonce")
}

func (s *Server) root(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not found", http.StatusNotFound)
}

func (s *Server) Serve() {
	s.writeCACert()

	mux := http.NewServeMux()
	mux.HandleFunc("/directory", s.directory)
	mux.HandleFunc("/new-nonce", s.newNonce)
	mux.HandleFunc("/new-acct", s.newAccount)
	mux.HandleFunc("/new-order", s.newOrder)
	mux.HandleFunc("/auth/", s.auth)
	mux.HandleFunc("/orders/", s.orders)
	mux.HandleFunc("/finalize/", s.finalize)
	mux.HandleFunc("/cert/", s.cert)
	mux.HandleFunc("/", s.root)

	http.Serve(s.lis, withLogging(mux))
}

func withLogging(parent http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("%s %s %s %s %s\n",
			time.Now().Format(time.StampMilli),
			r.RemoteAddr, r.Proto, r.Method, r.URL.String())
		parent.ServeHTTP(w, r)
	})
}

func readPayload(r *http.Request) []byte {
	// Body has a JSON with a "payload" message, which is a base64-encoded
	// JSON message with the actual content.
	req := struct{ Payload string }{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		panic(err)
	}

	payload, err := base64.RawURLEncoding.DecodeString(req.Payload)
	if err != nil {
		panic(err)
	}

	return payload
}

func decodePayload(r *http.Request, v interface{}) {
	payload := readPayload(r)
	err := json.Unmarshal(payload, v)
	if err != nil {
		panic(err)
	}
}

func logPayload(r *http.Request) {
	payload := readPayload(r)
	fmt.Printf("  %s\n", payload)
}

func main() {
	flag.Parse()

	srv, err := NewServer(*addr)
	if err != nil {
		panic(err)
	}

	fmt.Printf("%s/directory\n", srv.url())
	fmt.Printf("CA SN: %#v\n", srv.caTmpl.SerialNumber)
	srv.Serve()
}
