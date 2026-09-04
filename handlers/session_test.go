package handlers_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"corwinm/gottem.link/handlers"
)

const (
	sessionManagementToken = "test-management-token"
	sessionSecret          = "0123456789abcdef0123456789abcdef"
	sessionOrigin          = "https://admin.example.com"
)

func TestSessionCreationCookieAndState(t *testing.T) {
	now := time.Date(2026, time.September, 4, 12, 0, 0, 0, time.UTC)
	auth := handlers.NewSessionAuth(sessionManagementToken, sessionSecret, sessionOrigin, true, func() time.Time { return now })

	login := sessionRequest(auth.SessionHandler(), http.MethodPost, `{"token":"`+sessionManagementToken+`"}`, sessionOrigin, nil)
	if login.Code != http.StatusNoContent {
		t.Fatalf("login status/body = %d/%s, want 204", login.Code, login.Body.String())
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %#v, want one", cookies)
	}
	cookie := cookies[0]
	if cookie.Name != "gottem_session" || cookie.Path != "/" || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie flags = %#v", cookie)
	}
	wantExpiry := now.Add(8 * time.Hour)
	if cookie.MaxAge != int((8*time.Hour).Seconds()) || !cookie.Expires.Equal(wantExpiry) {
		t.Fatalf("cookie lifetime = MaxAge %d, Expires %v; want 8 hours ending %v", cookie.MaxAge, cookie.Expires, wantExpiry)
	}
	if strings.Contains(cookie.Value, sessionManagementToken) {
		t.Fatal("session cookie contains the management token")
	}
	payloadPart, _, ok := strings.Cut(cookie.Value, ".")
	if !ok {
		t.Fatal("session cookie has no signed payload separator")
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil {
		t.Fatal(err)
	}
	tokenHash := sha256.Sum256([]byte(sessionManagementToken))
	if strings.Contains(string(payload), hex.EncodeToString(tokenHash[:])) {
		t.Fatal("session cookie exposes an offline verifier for the management token")
	}
	if got := login.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("login Cache-Control = %q, want no-store", got)
	}

	state := sessionRequest(auth.SessionHandler(), http.MethodGet, "", "", cookie)
	if state.Code != http.StatusOK || state.Body.String() != "{\"authenticated\":true}\n" {
		t.Fatalf("state status/body = %d/%s", state.Code, state.Body.String())
	}
}

func TestSessionRejectsTamperExpiryAndManagementTokenRotation(t *testing.T) {
	now := time.Date(2026, time.September, 4, 12, 0, 0, 0, time.UTC)
	clock := now
	auth := handlers.NewSessionAuth(sessionManagementToken, sessionSecret, sessionOrigin, true, func() time.Time { return clock })
	login := sessionRequest(auth.SessionHandler(), http.MethodPost, `{"token":"`+sessionManagementToken+`"}`, sessionOrigin, nil)
	cookie := login.Result().Cookies()[0]

	tampered := *cookie
	last := tampered.Value[len(tampered.Value)-1]
	if last == 'A' {
		tampered.Value = tampered.Value[:len(tampered.Value)-1] + "B"
	} else {
		tampered.Value = tampered.Value[:len(tampered.Value)-1] + "A"
	}
	assertUnauthenticatedSession(t, auth, &tampered)

	clock = now.Add(8*time.Hour + time.Minute)
	assertUnauthenticatedSession(t, auth, cookie)

	clock = now
	rotated := handlers.NewSessionAuth("rotated-management-token", sessionSecret, sessionOrigin, true, func() time.Time { return clock })
	assertUnauthenticatedSession(t, rotated, cookie)
}

func TestSessionLoginAndLogoutRequireExactOrigin(t *testing.T) {
	auth := handlers.NewSessionAuth(sessionManagementToken, sessionSecret, sessionOrigin, true, time.Now)
	for _, origin := range []string{"", "https://evil.example.com", sessionOrigin + "/"} {
		login := sessionRequest(auth.SessionHandler(), http.MethodPost, `{"token":"`+sessionManagementToken+`"}`, origin, nil)
		if login.Code != http.StatusForbidden {
			t.Fatalf("login with origin %q status = %d, want 403", origin, login.Code)
		}
		logout := sessionRequest(auth.SessionHandler(), http.MethodDelete, "", origin, nil)
		if logout.Code != http.StatusForbidden {
			t.Fatalf("logout with origin %q status = %d, want 403", origin, logout.Code)
		}
	}

	wrongToken := sessionRequest(auth.SessionHandler(), http.MethodPost, `{"token":"backup-token"}`, sessionOrigin, nil)
	if wrongToken.Code != http.StatusUnauthorized || len(wrongToken.Result().Cookies()) != 0 {
		t.Fatalf("backup/wrong token login status/cookies = %d/%#v", wrongToken.Code, wrongToken.Result().Cookies())
	}
}

func TestBrowserOrBearerAuthOriginRules(t *testing.T) {
	auth := handlers.NewSessionAuth(sessionManagementToken, sessionSecret, sessionOrigin, true, time.Now)
	login := sessionRequest(auth.SessionHandler(), http.MethodPost, `{"token":"`+sessionManagementToken+`"}`, sessionOrigin, nil)
	cookie := login.Result().Cookies()[0]
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := auth.BrowserOrBearerAuth(next)

	cookieGet := authenticatedRequest(handler, http.MethodGet, "", "", cookie)
	if cookieGet.Code != http.StatusNoContent {
		t.Fatalf("same-origin cookie GET status = %d, want 204", cookieGet.Code)
	}
	for _, origin := range []string{"", "https://evil.example.com"} {
		cookieWrite := authenticatedRequest(handler, http.MethodPost, "", origin, cookie)
		if cookieWrite.Code != http.StatusForbidden {
			t.Fatalf("cookie POST origin %q status = %d, want 403", origin, cookieWrite.Code)
		}
	}
	cookieWrite := authenticatedRequest(handler, http.MethodPost, "", sessionOrigin, cookie)
	if cookieWrite.Code != http.StatusNoContent {
		t.Fatalf("cookie POST exact origin status = %d, want 204", cookieWrite.Code)
	}
	bearerWrite := authenticatedRequest(handler, http.MethodPost, "Bearer "+sessionManagementToken, "", nil)
	if bearerWrite.Code != http.StatusNoContent {
		t.Fatalf("bearer POST without Origin status = %d, want 204", bearerWrite.Code)
	}
}

func assertUnauthenticatedSession(t *testing.T, auth *handlers.SessionAuth, cookie *http.Cookie) {
	t.Helper()
	state := sessionRequest(auth.SessionHandler(), http.MethodGet, "", "", cookie)
	if state.Code != http.StatusOK || state.Body.String() != "{\"authenticated\":false}\n" {
		t.Fatalf("state status/body = %d/%s, want unauthenticated", state.Code, state.Body.String())
	}
}

func sessionRequest(handler http.Handler, method, body, origin string, cookie *http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "/api/v1/session", bytes.NewBufferString(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func authenticatedRequest(handler http.Handler, method, authorization, origin string, cookie *http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "/api/v1/redirects", nil)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}
