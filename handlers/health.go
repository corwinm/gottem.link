package handlers

import (
	"context"
	"net/http"
)

type readinessChecker interface {
	Ready(context.Context) error
}

func HealthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

func ReadinessHandler(database readinessChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := database.Ready(r.Context()); err != nil {
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		HealthHandler(w, r)
	}
}
