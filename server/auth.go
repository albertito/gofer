package server

import (
	"crypto/sha256"
	"encoding/hex"
	"io/ioutil"
	"math/rand"
	"net/http"
	"time"

	"blitiri.com.ar/go/gofer/trace"
	"gopkg.in/yaml.v3"
)

const authDuration = 10 * time.Millisecond

type AuthWrapper struct {
	handler http.Handler
	users   *AuthDB
}

func (a *AuthWrapper) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Make sure the call takes authDuration + 0-20% regardless of the
	// outcome, to prevent basic timing attacks.
	defer func(start time.Time) {
		elapsed := time.Since(start)
		delay := authDuration - elapsed
		if delay > 0 {
			maxDelta := int64(float64(delay) * 0.2)
			delay += time.Duration(rand.Int63n(maxDelta))
			time.Sleep(delay)
		}
	}(time.Now())

	tr, _ := trace.FromContext(r.Context())

	user, pass, ok := r.BasicAuth()
	if !ok {
		tr.Printf("auth header missing")
		a.failed(w)
		return
	}

	if dbPass, ok := a.users.Plain[user]; ok {
		if pass == dbPass {
			tr.Printf("auth for %q successful", user)
			a.handler.ServeHTTP(w, r)
		} else {
			tr.Printf("incorrect password (plain) for %q", user)
			a.failed(w)
		}
		return
	}

	if dbPass, ok := a.users.SHA256[user]; ok {
		// Take the sha256 of the given pass, and compare.
		buf := sha256.Sum256([]byte(pass))
		shaPass := hex.EncodeToString(buf[:])
		if shaPass == dbPass {
			tr.Printf("auth for %q successful", user)
			a.handler.ServeHTTP(w, r)
		} else {
			tr.Printf("incorrect password (sha256) for %q", user)
			a.failed(w)
		}
		return
	}

	tr.Printf("user %q not found", user)
	a.failed(w)
}

func (a *AuthWrapper) failed(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="Authentication"`)
	http.Error(w, "Unauthorized", http.StatusUnauthorized)
	return
}

type AuthDB struct {
	Plain  map[string]string
	SHA256 map[string]string
}

func LoadAuthFile(path string) (*AuthDB, error) {
	buf, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	db := &AuthDB{}
	err = yaml.Unmarshal(buf, &db)
	return db, err
}
