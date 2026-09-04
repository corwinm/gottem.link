package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"

	"corwinm/gottem.link/backup"
	"corwinm/gottem.link/db"
	"corwinm/gottem.link/validation"
)

const maxManagementBodyBytes = 1 << 20
const maxGeneratedSlugAttempts = 5

type createRedirectRequest struct {
	Slug *string `json:"slug"`
	URL  string  `json:"url"`
}

type updateRedirectRequest struct {
	URL string `json:"url"`
}

func ManagementCollectionHandler(database *db.DbWrapper, generateSlug SlugGenerator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			redirects, err := database.ListRedirects()
			if err != nil {
				writeManagementError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeManagementJSON(w, http.StatusOK, redirects)
		case http.MethodPost:
			var request createRedirectRequest
			if err := decodeManagementJSON(w, r, &request); err != nil {
				writeManagementError(w, http.StatusBadRequest, "invalid request")
				return
			}
			destination, err := validation.ValidateDestination(request.URL)
			if err != nil {
				writeManagementFieldError(w, http.StatusBadRequest, "invalid URL", "url")
				return
			}
			if request.Slug != nil {
				createCustomRedirect(w, database, *request.Slug, destination)
				return
			}
			createGeneratedRedirect(w, database, generateSlug, destination)
		default:
			w.Header().Set("Allow", "GET, POST")
			writeManagementError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
}

func createCustomRedirect(w http.ResponseWriter, database *db.DbWrapper, slug, destination string) {
	canonical, err := validation.ValidateSlug(slug)
	if err != nil {
		writeManagementFieldError(w, http.StatusBadRequest, "invalid slug", "slug")
		return
	}
	redirect, err := database.CreateRedirect(canonical, destination)
	switch {
	case errors.Is(err, db.ErrSlugConflict):
		writeManagementFieldError(w, http.StatusConflict, "slug already exists", "slug")
	case err != nil:
		writeManagementError(w, http.StatusInternalServerError, "internal server error")
	default:
		writeCreatedRedirect(w, redirect)
	}
}

func createGeneratedRedirect(w http.ResponseWriter, database *db.DbWrapper, generateSlug SlugGenerator, destination string) {
	for range maxGeneratedSlugAttempts {
		candidate, err := generateSlug()
		if err != nil {
			writeManagementError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		canonical, err := validation.ValidateSlug(candidate)
		if err != nil {
			writeManagementError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		redirect, err := database.CreateRedirect(canonical, destination)
		switch {
		case errors.Is(err, db.ErrSlugConflict):
			continue
		case err != nil:
			writeManagementError(w, http.StatusInternalServerError, "internal server error")
			return
		default:
			writeCreatedRedirect(w, redirect)
			return
		}
	}
	writeManagementFieldError(w, http.StatusConflict, "unable to generate unique slug", "slug")
}

func writeCreatedRedirect(w http.ResponseWriter, redirect db.Redirect) {
	w.Header().Set("Location", "/api/v1/redirects/"+url.PathEscape(redirect.Slug))
	writeManagementJSON(w, http.StatusCreated, redirect)
}

func ManagementItemHandler(database *db.DbWrapper) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slug := r.PathValue("slug")
		switch r.Method {
		case http.MethodGet:
			redirect, err := database.GetRedirect(slug)
			writeRedirectResult(w, redirect, err, http.StatusOK)
		case http.MethodPut:
			var request updateRedirectRequest
			if err := decodeManagementJSON(w, r, &request); err != nil {
				writeManagementError(w, http.StatusBadRequest, "invalid request")
				return
			}
			destination, err := validation.ValidateDestination(request.URL)
			if err != nil {
				writeManagementFieldError(w, http.StatusBadRequest, "invalid URL", "url")
				return
			}
			redirect, err := database.UpdateRedirect(slug, destination)
			writeRedirectResult(w, redirect, err, http.StatusOK)
		case http.MethodDelete:
			err := database.DeleteRedirect(slug)
			switch {
			case errors.Is(err, db.ErrRedirectNotFound):
				writeManagementError(w, http.StatusNotFound, "redirect not found")
			case err != nil:
				writeManagementError(w, http.StatusInternalServerError, "internal server error")
			default:
				w.WriteHeader(http.StatusNoContent)
			}
		default:
			w.Header().Set("Allow", "GET, PUT, DELETE")
			writeManagementError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
}

func ManagementDisableHandler(database *db.DbWrapper) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeManagementError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		redirect, err := database.DisableRedirect(r.PathValue("slug"))
		writeRedirectResult(w, redirect, err, http.StatusOK)
	})
}

func ManagementImportHandler(database *db.DbWrapper) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeManagementError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		envelope, err := backup.Decode(r.Body)
		if err != nil {
			var validationErr *backup.ValidationError
			if errors.As(err, &validationErr) {
				writeManagementJSON(w, http.StatusBadRequest, struct {
					Error  string         `json:"error"`
					Issues []backup.Issue `json:"issues"`
				}{Error: "invalid redirects", Issues: validationErr.Issues})
				return
			}
			writeManagementError(w, http.StatusBadRequest, "invalid import")
			return
		}

		redirects := make([]db.ImportRedirect, len(envelope.Redirects))
		for index, redirect := range envelope.Redirects {
			redirects[index] = db.ImportRedirect{Slug: redirect.Slug, URL: redirect.URL, Disabled: redirect.Disabled}
		}
		if r.URL.Query().Get("dry_run") == "true" {
			conflicts, err := database.RedirectImportConflicts(redirects)
			if err != nil {
				writeManagementError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			if len(conflicts) > 0 {
				writeImportConflicts(w, conflicts)
				return
			}
			writeImportResult(w, len(redirects), 0)
			return
		}
		if err := database.ImportRedirects(redirects); err != nil {
			var conflicts *db.SlugConflictsError
			if errors.As(err, &conflicts) {
				writeImportConflicts(w, conflicts.Slugs)
				return
			}
			writeManagementError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeImportResult(w, len(redirects), len(redirects))
	})
}

func writeImportConflicts(w http.ResponseWriter, conflicts []string) {
	writeManagementJSON(w, http.StatusConflict, struct {
		Error     string   `json:"error"`
		Conflicts []string `json:"conflicts"`
	}{Error: "slug conflicts", Conflicts: conflicts})
}

func writeImportResult(w http.ResponseWriter, total, imported int) {
	writeManagementJSON(w, http.StatusOK, struct {
		Total    int `json:"total"`
		Imported int `json:"imported"`
	}{Total: total, Imported: imported})
}

func writeRedirectResult(w http.ResponseWriter, redirect db.Redirect, err error, successStatus int) {
	switch {
	case errors.Is(err, db.ErrRedirectNotFound):
		writeManagementError(w, http.StatusNotFound, "redirect not found")
	case err != nil:
		writeManagementError(w, http.StatusInternalServerError, "internal server error")
	default:
		writeManagementJSON(w, successStatus, redirect)
	}
}

func decodeManagementJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxManagementBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON value")
	}
	return nil
}

func writeManagementError(w http.ResponseWriter, status int, message string) {
	writeManagementJSON(w, status, map[string]string{"error": message})
}

func writeManagementFieldError(w http.ResponseWriter, status int, message, field string) {
	writeManagementJSON(w, status, map[string]string{"error": message, "field": field})
}

func writeManagementJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
