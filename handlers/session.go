package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	sessionCookieName = "gottem_session"
	sessionLifetime   = 8 * time.Hour
	maxSessionBody    = 8 << 10
)

type SessionAuth struct {
	managementTokenHash [sha256.Size]byte
	tokenFingerprint    [sha256.Size]byte
	secret              []byte
	origin              string
	secureCookies       bool
	now                 func() time.Time
}

func NewSessionAuth(managementToken, sessionSecret, origin string, secureCookies bool, now func() time.Time) *SessionAuth {
	if now == nil {
		now = time.Now
	}
	secret := []byte(sessionSecret)
	fingerprintMAC := hmac.New(sha256.New, secret)
	_, _ = fingerprintMAC.Write([]byte("management-token-fingerprint\x00"))
	_, _ = fingerprintMAC.Write([]byte(managementToken))
	var fingerprint [sha256.Size]byte
	copy(fingerprint[:], fingerprintMAC.Sum(nil))
	return &SessionAuth{
		managementTokenHash: sha256.Sum256([]byte(managementToken)),
		tokenFingerprint:    fingerprint,
		secret:              secret,
		origin:              origin,
		secureCookies:       secureCookies,
		now:                 now,
	}
}

func (auth *SessionAuth) SessionHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		switch r.Method {
		case http.MethodGet:
			writeManagementJSON(w, http.StatusOK, struct {
				Authenticated bool `json:"authenticated"`
			}{Authenticated: auth.hasValidSession(r)})
		case http.MethodPost:
			if !auth.hasExpectedOrigin(r) {
				writeManagementError(w, http.StatusForbidden, "forbidden")
				return
			}
			var request struct {
				Token string `json:"token"`
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxSessionBody)
			decoder := json.NewDecoder(r.Body)
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&request); err != nil || request.Token == "" {
				writeManagementError(w, http.StatusBadRequest, "invalid request")
				return
			}
			if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
				writeManagementError(w, http.StatusBadRequest, "invalid request")
				return
			}
			actual := sha256.Sum256([]byte(request.Token))
			if subtle.ConstantTimeCompare(auth.managementTokenHash[:], actual[:]) != 1 {
				writeManagementError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			auth.setSessionCookie(w)
			w.WriteHeader(http.StatusNoContent)
		case http.MethodDelete:
			if !auth.hasExpectedOrigin(r) {
				writeManagementError(w, http.StatusForbidden, "forbidden")
				return
			}
			auth.clearSessionCookie(w)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.Header().Set("Allow", "GET, POST, DELETE")
			writeManagementError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
}

func (auth *SessionAuth) BrowserOrBearerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Header.Get("Authorization") != "" {
			// Verify against the original token hash without treating the hash as a token.
			if !auth.hasValidBearer(r) {
				w.Header().Set("WWW-Authenticate", "Bearer")
				writeManagementError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		if !auth.hasValidSession(r) {
			writeManagementError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if isUnsafeMethod(r.Method) && !auth.hasExpectedOrigin(r) {
			writeManagementError(w, http.StatusForbidden, "forbidden")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (auth *SessionAuth) hasValidBearer(r *http.Request) bool {
	scheme, supplied, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	actual := sha256.Sum256([]byte(supplied))
	return ok && strings.EqualFold(scheme, "Bearer") && supplied != "" && subtle.ConstantTimeCompare(auth.managementTokenHash[:], actual[:]) == 1
}

func (auth *SessionAuth) hasExpectedOrigin(r *http.Request) bool {
	actual := []byte(r.Header.Get("Origin"))
	expected := []byte(auth.origin)
	return len(actual) == len(expected) && subtle.ConstantTimeCompare(actual, expected) == 1
}

func (auth *SessionAuth) setSessionCookie(w http.ResponseWriter) {
	expires := auth.now().Add(sessionLifetime)
	payload := strconv.FormatInt(expires.Unix(), 10) + "." + hex.EncodeToString(auth.tokenFingerprint[:])
	mac := hmac.New(sha256.New, auth.secret)
	_, _ = mac.Write([]byte(payload))
	value := base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(sessionLifetime.Seconds()),
		HttpOnly: true,
		Secure:   auth.secureCookies,
		SameSite: http.SameSiteStrictMode,
	})
}

func (auth *SessionAuth) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Path:     "/",
		Expires:  time.Unix(1, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   auth.secureCookies,
		SameSite: http.SameSiteStrictMode,
	})
}

func (auth *SessionAuth) hasValidSession(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return false
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, auth.secret)
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return false
	}
	payloadParts := strings.Split(string(payload), ".")
	if len(payloadParts) != 2 {
		return false
	}
	expires, err := strconv.ParseInt(payloadParts[0], 10, 64)
	if err != nil || auth.now().Unix() >= expires {
		return false
	}
	fingerprint, err := hex.DecodeString(payloadParts[1])
	if err != nil || len(fingerprint) != sha256.Size {
		return false
	}
	return subtle.ConstantTimeCompare(fingerprint, auth.tokenFingerprint[:]) == 1
}

func isUnsafeMethod(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}
