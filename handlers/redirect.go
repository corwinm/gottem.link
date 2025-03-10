package handlers

import (
	"fmt"
	"net/http"
	"strings"
)

func RedirectHandler(w http.ResponseWriter, r *http.Request) {
	rawSlug := r.URL.Path[1:]
	if rawSlug == "" {
		http.Error(w, "No slug provided", http.StatusBadRequest)
		return
	}
	slug := strings.ToLower(rawSlug)
	fmt.Fprintln(w, "Redirecting to:", slug)
}
