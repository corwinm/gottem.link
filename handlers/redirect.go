package handlers

import (
	"corwinm/gottem.link/db"
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

	gottemDb, err := db.GetDB("/litefs/gottem.db")
	if err != nil {
		fmt.Println("Error loading DB: ", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer gottemDb.Close()

	url, err := gottemDb.QuerySlug(slug)
	if err != nil {
		http.Error(w, "Slug not found", http.StatusNotFound)
		return
	}

	fmt.Fprintln(w, "Redirecting to:", slug)
	http.Redirect(w, r, url, http.StatusFound)
}
