package handlers

import (
	"corwinm/gottem.link/db"
	"database/sql"
	"embed"
	"errors"
	"net/http"
)

//go:embed admin/index.html admin/admin.css admin/admin.js
var adminFiles embed.FS

const adminCSP = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

func AdminPageHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		setAdminSecurityHeaders(w)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		serveEmbeddedAdminFile(w, r, "admin/index.html")
	})
}

func AdminPageOrLegacyRedirectHandler(database *db.DbWrapper) http.Handler {
	return adminPageOrLegacyRedirectHandler(database, AdminPageHandler())
}

func AdminLegacyRedirectHandler(database *db.DbWrapper) http.Handler {
	return adminPageOrLegacyRedirectHandler(database, http.NotFoundHandler())
}

func adminPageOrLegacyRedirectHandler(database *db.DbWrapper, fallback http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirect, err := database.GetRedirect("admin")
		switch {
		case errors.Is(err, db.ErrRedirectNotFound):
			fallback.ServeHTTP(w, r)
		case err != nil:
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		case redirect.DisabledAt != nil:
			http.Error(w, "Slug not found", http.StatusNotFound)
		default:
			destination, err := database.QuerySlug("admin")
			switch {
			case errors.Is(err, sql.ErrNoRows):
				http.Error(w, "Slug not found", http.StatusNotFound)
			case err != nil:
				http.Error(w, "Internal server error", http.StatusInternalServerError)
			default:
				http.Redirect(w, r, destination, http.StatusFound)
			}
		}
	})
}

func AdminAssetHandler(name, contentType string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		setAdminSecurityHeaders(w)
		w.Header().Set("Content-Type", contentType)
		serveEmbeddedAdminFile(w, r, "admin/"+name)
	})
}

func setAdminSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy", adminCSP)
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
}

func serveEmbeddedAdminFile(w http.ResponseWriter, r *http.Request, name string) {
	content, err := adminFiles.ReadFile(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Length", "")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(content)
}
