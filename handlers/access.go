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
	"corwinm/gottem.link/validation"
)

const maxAccessRequestBytes = 1024

type internalAccessRequest struct {
	RedirectID int64  `json:"redirect_id"`
	AccessedAt string `json:"accessed_at"`
}

type internalMissRequest struct {
	Slug     string `json:"slug"`
	MissedAt string `json:"missed_at"`
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

func InternalMissHandler(store db.MissStore, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token == "" || r.Method != http.MethodPost || !requestIsLoopback(r) || !accessTokenMatches(r, token) {
			http.NotFound(w, r)
			return
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAccessRequestBytes))
		decoder.DisallowUnknownFields()
		var request internalMissRequest
		if err := decoder.Decode(&request); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
			http.Error(w, "invalid miss", http.StatusBadRequest)
			return
		}
		slug, err := validation.ValidateSlug(request.Slug)
		if err != nil || slug != request.Slug {
			http.Error(w, "invalid miss", http.StatusBadRequest)
			return
		}
		missedAt, err := time.Parse(time.RFC3339Nano, request.MissedAt)
		if err != nil {
			http.Error(w, "invalid miss", http.StatusBadRequest)
			return
		}
		if err := store.RecordSlugMiss(r.Context(), slug, missedAt); err != nil {
			http.Error(w, "record miss", http.StatusInternalServerError)
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
