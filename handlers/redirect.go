package handlers

import (
	"corwinm/gottem.link/db"
	"database/sql"
	"embed"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"corwinm/gottem.link/validation"
)

//go:embed missing/index.html
var missingFiles embed.FS

type AccessTracker interface {
	Track(id int64, at time.Time) bool
}

type MissTracker interface {
	TrackMiss(slug string, at time.Time) bool
}

func RedirectHandler(database *db.DbWrapper, trackers ...AccessTracker) http.HandlerFunc {
	var tracker AccessTracker
	var missTracker MissTracker
	if len(trackers) > 0 {
		tracker = trackers[0]
		missTracker, _ = trackers[0].(MissTracker)
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
			if canonical, validationErr := validation.ValidateSlug(rawSlug); validationErr == nil && missTracker != nil {
				_, existingErr := database.GetRedirect(canonical)
				switch {
				case errors.Is(existingErr, db.ErrRedirectNotFound):
					missTracker.TrackMiss(canonical, time.Now().UTC())
				case existingErr != nil:
					log.Printf("classify missing slug %q: %v", canonical, existingErr)
				}
			}
			MissingPageHandler(w, r)
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

func MissingPageHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	if r.Method == http.MethodHead {
		return
	}
	content, err := missingFiles.ReadFile("missing/index.html")
	if err != nil {
		return
	}
	_, _ = w.Write(content)
}
