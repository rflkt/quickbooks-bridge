package main

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// requireBearer guards the MCP endpoint with a static bearer token when one is
// configured. Without a token the server only listens on localhost (see main),
// which is the safe default for a single person trying it out. A deployment
// that serves several people should put per-user OAuth in front instead, so
// every call can be checked against the person behind it.
func requireBearer(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(strings.TrimSpace(r.Header.Get("Authorization")))
		if subtle.ConstantTimeCompare(got, want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="quickbooks-bridge"`)
			http.Error(w, "missing or wrong bearer token", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
