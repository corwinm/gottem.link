package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"corwinm/gottem.link/db"
)

const maxManagementBodyBytes = 1 << 20

type createRedirectRequest struct {
	Slug string `json:"slug"`
	URL  string `json:"url"`
}

type updateRedirectRequest struct {
	URL string `json:"url"`
}

func ManagementCollectionHandler(database *db.DbWrapper) http.Handler {
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
			if err := decodeManagementJSON(w, r, &request); err != nil || strings.TrimSpace(request.Slug) == "" || strings.TrimSpace(request.URL) == "" {
				writeManagementError(w, http.StatusBadRequest, "invalid request")
				return
			}
			redirect, err := database.CreateRedirect(request.Slug, request.URL)
			switch {
			case errors.Is(err, db.ErrSlugConflict):
				writeManagementError(w, http.StatusConflict, "slug already exists")
			case err != nil:
				writeManagementError(w, http.StatusInternalServerError, "internal server error")
			default:
				w.Header().Set("Location", "/api/v1/redirects/"+url.PathEscape(redirect.Slug))
				writeManagementJSON(w, http.StatusCreated, redirect)
			}
		default:
			w.Header().Set("Allow", "GET, POST")
			writeManagementError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
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
			if err := decodeManagementJSON(w, r, &request); err != nil || strings.TrimSpace(request.URL) == "" {
				writeManagementError(w, http.StatusBadRequest, "invalid request")
				return
			}
			redirect, err := database.UpdateRedirect(slug, request.URL)
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

func writeManagementJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
