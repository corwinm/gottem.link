package handlers

import (
	"corwinm/gottem.link/db"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"
)

type AccessTracker interface {
	Track(id int64, at time.Time) bool
}

func RedirectHandler(database *db.DbWrapper, trackers ...AccessTracker) http.HandlerFunc {
	var tracker AccessTracker
	if len(trackers) > 0 {
		tracker = trackers[0]
	}
	return func(w http.ResponseWriter, r *http.Request) {
		rawSlug := r.URL.Path[1:]
		if rawSlug == "" {
			http.Error(w, "No slug provided", http.StatusBadRequest)
			return
		}
		slug := strings.ToLower(rawSlug)

		id, url, err := database.ResolveSlug(slug)
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
		if tracker != nil {
			tracker.Track(id, time.Now().UTC())
		}
	}
}
