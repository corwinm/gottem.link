package handlers

import (
	"corwinm/gottem.link/db"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"
)

func RedirectHandler(database *db.DbWrapper) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rawSlug := r.URL.Path[1:]
		if rawSlug == "" {
			http.Error(w, "No slug provided", http.StatusBadRequest)
			return
		}
		slug := strings.ToLower(rawSlug)

		url, err := database.QuerySlug(slug)
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "Slug not found", http.StatusNotFound)
			return
		}
		if err != nil {
			log.Printf("query slug %q: %v", slug, err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, url, http.StatusFound)
	}
}
