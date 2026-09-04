package handlers

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
)

func BearerAuth(token string, next http.Handler) http.Handler {
	return BearerAuthAny([]string{token}, next)
}

func BearerAuthAny(tokens []string, next http.Handler) http.Handler {
	expected := make([][sha256.Size]byte, 0, len(tokens))
	for _, token := range tokens {
		if token != "" {
			expected = append(expected, sha256.Sum256([]byte(token)))
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, supplied, ok := strings.Cut(r.Header.Get("Authorization"), " ")
		actual := sha256.Sum256([]byte(supplied))
		authorized := 0
		for index := range expected {
			authorized |= subtle.ConstantTimeCompare(expected[index][:], actual[:])
		}
		if len(expected) == 0 || !ok || !strings.EqualFold(scheme, "Bearer") || supplied == "" || authorized != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeManagementError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}
