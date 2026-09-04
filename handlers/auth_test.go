package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"corwinm/gottem.link/handlers"
)

func TestBearerAuth(t *testing.T) {
	const token = "test-management-token"
	tests := []struct {
		name          string
		configured    string
		authorization string
		wantStatus    int
	}{
		{name: "missing header", configured: token, wantStatus: http.StatusUnauthorized},
		{name: "wrong scheme", configured: token, authorization: "Basic " + token, wantStatus: http.StatusUnauthorized},
		{name: "missing bearer value", configured: token, authorization: "Bearer", wantStatus: http.StatusUnauthorized},
		{name: "wrong token", configured: token, authorization: "Bearer wrong", wantStatus: http.StatusUnauthorized},
		{name: "empty configured token fails closed", authorization: "Bearer anything", wantStatus: http.StatusUnauthorized},
		{name: "valid token", configured: token, authorization: "bearer " + token, wantStatus: http.StatusNoContent},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			})
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/api/v1/redirects", nil)
			if test.authorization != "" {
				request.Header.Set("Authorization", test.authorization)
			}

			handlers.BearerAuth(test.configured, next).ServeHTTP(recorder, request)

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
			if recorder.Code == http.StatusUnauthorized {
				if got := recorder.Header().Get("WWW-Authenticate"); got != "Bearer" {
					t.Errorf("WWW-Authenticate = %q, want Bearer", got)
				}
				if strings.Contains(recorder.Body.String(), token) {
					t.Fatal("response leaked the configured token")
				}
				if got := recorder.Header().Get("Content-Type"); got != "application/json" {
					t.Fatalf("Content-Type = %q, want application/json", got)
				}
				if got := recorder.Body.String(); got != "{\"error\":\"unauthorized\"}\n" {
					t.Fatalf("body = %q, want JSON authentication error", got)
				}
			}
		})
	}
}

func TestBearerAuthAny(t *testing.T) {
	for name, authorization := range map[string]string{
		"first token":  "Bearer management-token",
		"second token": "Bearer backup-token",
	} {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/api/v1/exports", nil)
			request.Header.Set("Authorization", authorization)
			handlers.BearerAuthAny([]string{"management-token", "backup-token"}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			})).ServeHTTP(recorder, request)
			if recorder.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want 204", recorder.Code)
			}
		})
	}
}
