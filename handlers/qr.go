package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"corwinm/gottem.link/db"
	go_qr "github.com/piglig/go-qr"
)

const qrCSP = "default-src 'none'; frame-ancestors 'none'; sandbox"

var qrImageConfig = go_qr.NewQrCodeImgConfig(8, 4)

func ManagementQRCodeHandler(database *db.DbWrapper, origin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setQRCodeSecurityHeaders(w)
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeManagementError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		redirect, err := database.GetRedirect(r.PathValue("slug"))
		switch {
		case errors.Is(err, db.ErrRedirectNotFound):
			writeManagementError(w, http.StatusNotFound, "redirect not found")
			return
		case err != nil:
			writeManagementError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		shortURL := origin + "/" + url.PathEscape(redirect.Slug)
		code, err := go_qr.EncodeText(shortURL, go_qr.High)
		if err != nil {
			writeManagementError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		image, err := code.ToPNGBytes(qrImageConfig)
		if err != nil {
			writeManagementError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Length", strconv.Itoa(len(image)))
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write(image)
		}
	})
}

func setQRCodeSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", qrCSP)
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
}
