package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"strings"
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
			a.success(w, r, user)
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
			a.success(w, r, user)
		} else {
			tr.Printf("incorrect password (sha256) for %q", user)
			a.failed(w)
		}
		return
	}

	tr.Printf("user %q not found", user)
	a.failed(w)
}

// success is called after a successful authentication. It associates the
// authenticated user with the request context (so per-user handlers can read
// it, see UserFromContext) and invokes the wrapped handler.
func (a *AuthWrapper) success(w http.ResponseWriter, r *http.Request, user string) {
	tr, _ := trace.FromContext(r.Context())
	tr.Printf("auth for %q successful", user)
	r = r.WithContext(withUser(r.Context(), user))
	a.handler.ServeHTTP(w, r)
}

func (a *AuthWrapper) failed(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="Authentication"`)
	http.Error(w, "Unauthorized", http.StatusUnauthorized)
	return
}

type userCtxKey struct{}

// withUser returns a copy of ctx carrying the authenticated user name.
func withUser(ctx context.Context, user string) context.Context {
	return context.WithValue(ctx, userCtxKey{}, user)
}

// UserFromContext returns the authenticated user name associated with the
// request context. It is set by AuthWrapper on successful authentication, so
// it is only present on routes covered by auth.
func UserFromContext(ctx context.Context) (string, bool) {
	user, ok := ctx.Value(userCtxKey{}).(string)
	return user, ok
}

// validUser reports an error if name is not safe to use as a single on-disk
// path component. Per-user directory routes derive a path from the
// authenticated user (see makeDir), so we reject anything that could escape or
// alter the directory structure. Checked at load time, which guarantees a
// loaded AuthDB only contains path-safe user names.
func validUser(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("empty user name")
	case name == "." || name == "..":
		return fmt.Errorf("invalid user name %q", name)
	case strings.ContainsAny(name, `/\`+"\x00"):
		return fmt.Errorf("user name %q contains an invalid character", name)
	}
	return nil
}

type AuthDB struct {
	Plain  map[string]string
	SHA256 map[string]string
}

func LoadAuthFile(path string) (*AuthDB, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	db := &AuthDB{}
	if err := yaml.Unmarshal(buf, &db); err != nil {
		return nil, err
	}

	for _, users := range []map[string]string{db.Plain, db.SHA256} {
		for user := range users {
			if err := validUser(user); err != nil {
				return nil, fmt.Errorf("%s: %v", path, err)
			}
		}
	}

	return db, nil
}
