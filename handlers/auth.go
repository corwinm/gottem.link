package handlers

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
)

func BearerAuth(token string, next http.Handler) http.Handler {
	expected := sha256.Sum256([]byte(token))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, supplied, ok := strings.Cut(r.Header.Get("Authorization"), " ")
		actual := sha256.Sum256([]byte(supplied))
		if token == "" || !ok || !strings.EqualFold(scheme, "Bearer") || supplied == "" || subtle.ConstantTimeCompare(expected[:], actual[:]) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeManagementError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}
