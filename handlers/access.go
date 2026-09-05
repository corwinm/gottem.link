package handlers

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"corwinm/gottem.link/db"
)

const maxAccessRequestBytes = 1024

type internalAccessRequest struct {
	RedirectID int64  `json:"redirect_id"`
	AccessedAt string `json:"accessed_at"`
}

func InternalAccessHandler(store db.AccessStore, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token == "" || r.Method != http.MethodPost || !requestIsLoopback(r) || !accessTokenMatches(r, token) {
			http.NotFound(w, r)
			return
		}

		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAccessRequestBytes))
		decoder.DisallowUnknownFields()
		var request internalAccessRequest
		if err := decoder.Decode(&request); err != nil || decoder.Decode(&struct{}{}) != io.EOF || request.RedirectID <= 0 {
			http.Error(w, "invalid access", http.StatusBadRequest)
			return
		}
		accessedAt, err := time.Parse(time.RFC3339Nano, request.AccessedAt)
		if err != nil {
			http.Error(w, "invalid access", http.StatusBadRequest)
			return
		}
		if err := store.RecordRedirectAccess(r.Context(), request.RedirectID, accessedAt); err != nil {
			http.Error(w, "record access", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func requestIsLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func accessTokenMatches(r *http.Request, token string) bool {
	provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return len(provided) == len(token) && subtle.ConstantTimeCompare([]byte(provided), []byte(token)) == 1
}
