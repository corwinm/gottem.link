package routes_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"corwinm/gottem.link/db"
	"corwinm/gottem.link/routes"
)

const testSessionSecret = "0123456789abcdef0123456789abcdef"

func TestAdminRoutesDisabledUnlessFullyConfigured(t *testing.T) {
	database := testDatabase(t)
	configs := []routes.AdminConfig{
		{},
		{Origin: "https://admin.example.com"},
		{SessionSecret: testSessionSecret},
		{Origin: "https://admin.example.com", SessionSecret: "too-short", SecureCookies: true},
		{Origin: "http://admin.example.com", SessionSecret: testSessionSecret},
		{Origin: "https://admin.example.com", SessionSecret: testSessionSecret, SecureCookies: false},
		{Origin: "https://admin.example.com:443", SessionSecret: testSessionSecret, SecureCookies: true},
	}
	for _, config := range configs {
		router := routes.NewRouterWithAdmin(database, testManagementToken, "", config)
		for _, path := range []string{"/admin", "/admin/assets/admin.css", "/api/v1/session"} {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != http.StatusNotFound {
				t.Fatalf("config %#v path %s status = %d, want 404", config, path, response.Code)
			}
		}
		bearer := managementRequest(t, router, http.MethodGet, "/api/v1/redirects", "", testManagementToken)
		if bearer.Code != http.StatusOK {
			t.Fatalf("bearer API status with disabled admin = %d, want 200", bearer.Code)
		}
	}
}

func TestAdminPageAndAssetsSecurityAndAccessibilityContracts(t *testing.T) {
	database := testDatabase(t)
	router := routes.NewRouterWithAdmin(database, testManagementToken, "", routes.AdminConfig{
		Origin:        "https://admin.example.com",
		SessionSecret: testSessionSecret,
		SecureCookies: true,
	})

	page := httptest.NewRecorder()
	router.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if page.Code != http.StatusOK || page.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("admin status/content-type = %d/%q", page.Code, page.Header().Get("Content-Type"))
	}
	assertSecurityHeaders(t, page.Header())
	markup := page.Body.String()
	for _, required := range []string{
		`<main`, `id="login-form"`, `type="password"`, `autocomplete="current-password"`,
		`brand-wordmark`, `gottem<span>.link</span>`, `id="create-form"`, `id="create-expiration"`, `type="datetime-local"`,
		`id="search"`, `role="status"`, `aria-live="polite"`, `<template id="redirect-template"`,
		`class="expiration-detail"`, `class="destination-updated"`, `class="usage-count"`, `class="last-accessed"`, `aria-label="Usage statistics"`, `class="button quiet qr"`, `class="button quiet expiration"`,
		`id="qr-dialog"`, `aria-labelledby="qr-title"`, `id="qr-image"`, `alt=""`, `id="qr-url"`, `id="qr-status"`, `role="status"`, `id="qr-error"`, `role="alert"`, `id="qr-download"`, `download`, `data-close-qr`,
		`id="expiration-dialog"`, `id="expiration-form"`, `id="expiration-value"`, `step="0.001"`, `id="expiration-error"`, `role="alert"`, `id="clear-expiration"`,
		`id="delete-dialog"`, `<script src="/admin/assets/admin.js" defer></script>`,
		`<link rel="stylesheet" href="/admin/assets/admin.css">`,
	} {
		if !strings.Contains(markup, required) {
			t.Errorf("admin markup missing %q", required)
		}
	}
	if strings.Contains(markup, "<style") || strings.Contains(markup, "<script>") {
		t.Fatal("admin markup contains inline style or script")
	}

	for _, asset := range []struct {
		path        string
		contentType string
		contains    []string
	}{
		{path: "/admin/assets/admin.css", contentType: "text/css; charset=utf-8", contains: []string{"--paper: #eeeae1", "--ink: #26251f", "--accent: #e3b94e", "--signal: #765400", "Arial Black", "Georgia", "Courier New", ".button.header-button", "min-height: 44px", ":focus-visible", ".status.expired", ".qr-image", ".qr-url", "@media (max-width:", "prefers-reduced-motion"}},
		{path: "/admin/assets/admin.js", contentType: "text/javascript; charset=utf-8", contains: []string{"textContent", "navigator.clipboard", "fetch(", "confirmDelete", "redirectStatus", "expires_at", "destination_updated_at", "click_count", "last_accessed_at", "approximately", "openQRCodeDialog", "qrDialog.showModal()", "expirationDialog.showModal()"}},
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, asset.path, nil))
		if response.Code != http.StatusOK || response.Header().Get("Content-Type") != asset.contentType {
			t.Fatalf("%s status/content-type = %d/%q", asset.path, response.Code, response.Header().Get("Content-Type"))
		}
		assertSecurityHeaders(t, response.Header())
		for _, required := range asset.contains {
			if !strings.Contains(response.Body.String(), required) {
				t.Errorf("%s missing %q", asset.path, required)
			}
		}
		if asset.path == "/admin/assets/admin.js" {
			if strings.Contains(response.Body.String(), "innerHTML") {
				t.Fatal("admin JavaScript uses innerHTML")
			}
			for _, contract := range []string{
				`deleteDialog.returnValue = "cancel"`,
				"const shortURL = `${location.origin}/${encodeURIComponent(redirect.slug)}`",
				"`${value.replace(\" \", \"T\")}Z`",
				"originalExpirationValue",
				"scheduleStatusRefresh()",
				`document.addEventListener("visibilitychange"`,
				"expirationError.textContent = error.message",
				"if (qrDialog.open) qrDialog.close()",
				"resetQRCodeDialog()",
				"const imageURL = `/api/v1/redirects/${encodeURIComponent(redirect.slug)}/qr.png`",
				"qrImage.addEventListener(\"load\"",
				"qrImage.addEventListener(\"error\"",
				"qrDownload.download = `${redirect.slug}-qr.png`",
				"await authenticatedFetch(imageURL)",
				"await response.blob()",
				"URL.createObjectURL(blob)",
				"URL.revokeObjectURL(qrObjectURL)",
				"qrDownload.href = qrObjectURL",
				"qrImage.src = qrObjectURL",
				"qrImage.removeAttribute(\"src\")",
				"if (expirationDialog.open) expirationDialog.close()",
				"catch (error) {\n    setNotice(error.message);\n    return;\n  }\n  redirects = [];",
			} {
				if !strings.Contains(response.Body.String(), contract) {
					t.Errorf("admin JavaScript missing safety contract %q", contract)
				}
			}
		}
	}
}

func TestAdminPreservesLegacyAdminRedirect(t *testing.T) {
	database := testDatabase(t)
	if _, err := database.CreateRedirect("admin", "https://example.com/legacy-admin"); err != nil {
		t.Fatal(err)
	}
	router := routes.NewRouterWithAdmin(database, testManagementToken, "", routes.AdminConfig{
		Origin: "https://admin.example.com", SessionSecret: testSessionSecret, SecureCookies: true,
	})

	legacy := httptest.NewRecorder()
	router.ServeHTTP(legacy, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if legacy.Code != http.StatusFound || legacy.Header().Get("Location") != "https://example.com/legacy-admin" {
		t.Fatalf("legacy /admin status/location = %d/%q", legacy.Code, legacy.Header().Get("Location"))
	}
	console := httptest.NewRecorder()
	router.ServeHTTP(console, httptest.NewRequest(http.MethodGet, "/admin/", nil))
	if console.Code != http.StatusOK || !strings.Contains(console.Body.String(), `id="login-form"`) {
		t.Fatalf("fallback console status/body = %d/%q", console.Code, console.Body.String())
	}

	past := "2000-01-01T00:00:00Z"
	if _, err := database.SetRedirectExpiration("admin", &past); err != nil {
		t.Fatal(err)
	}
	expired := httptest.NewRecorder()
	router.ServeHTTP(expired, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if expired.Code != http.StatusNotFound {
		t.Fatalf("expired legacy /admin status = %d, want 404", expired.Code)
	}
}

func TestDisabledAdminPreservesLegacyAdminRedirect(t *testing.T) {
	database := testDatabase(t)
	if _, err := database.CreateRedirect("admin", "https://example.com/legacy-admin"); err != nil {
		t.Fatal(err)
	}
	router := routes.NewRouterWithAdmin(database, testManagementToken, "", routes.AdminConfig{})

	legacy := httptest.NewRecorder()
	router.ServeHTTP(legacy, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if legacy.Code != http.StatusFound || legacy.Header().Get("Location") != "https://example.com/legacy-admin" {
		t.Fatalf("legacy /admin status/location with disabled UI = %d/%q", legacy.Code, legacy.Header().Get("Location"))
	}
}

func TestAdminCookieEndToEndCRUD(t *testing.T) {
	database := testDatabase(t)
	server := httptest.NewUnstartedServer(nil)
	origin := "http://" + server.Listener.Addr().String()
	server.Config.Handler = routes.NewRouterWithAdmin(database, testManagementToken, "test-backup-token", routes.AdminConfig{
		Origin:        origin,
		SessionSecret: testSessionSecret,
		SecureCookies: false,
	})
	server.Start()
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := server.Client()
	client.Jar = jar

	login := browserAPIRequest(t, client, origin, http.MethodPost, "/api/v1/session", `{"token":"`+testManagementToken+`"}`, true)
	if login.StatusCode != http.StatusNoContent {
		t.Fatalf("login status/body = %d/%s", login.StatusCode, readBody(t, login))
	}
	login.Body.Close()
	parsedOrigin, _ := url.Parse(origin)
	if len(jar.Cookies(parsedOrigin)) != 1 {
		t.Fatalf("cookie jar = %#v, want session cookie", jar.Cookies(parsedOrigin))
	}

	created := browserAPIRequest(t, client, origin, http.MethodPost, "/api/v1/redirects", `{"slug":"ui-link","url":"https://example.com/one"}`, true)
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create status/body = %d/%s", created.StatusCode, readBody(t, created))
	}
	created.Body.Close()

	listed := browserAPIRequest(t, client, origin, http.MethodGet, "/api/v1/redirects", "", false)
	var redirects []db.Redirect
	decodeResponseJSON(t, listed, &redirects)
	if listed.StatusCode != http.StatusOK || len(redirects) != 1 || redirects[0].Slug != "ui-link" {
		t.Fatalf("list status/redirects = %d/%#v", listed.StatusCode, redirects)
	}

	for _, step := range []struct {
		method string
		path   string
		body   string
		active bool
	}{
		{method: http.MethodPut, path: "/api/v1/redirects/ui-link", body: `{"url":"https://example.com/two"}`, active: true},
		{method: http.MethodPost, path: "/api/v1/redirects/ui-link/disable", active: false},
		{method: http.MethodPost, path: "/api/v1/redirects/ui-link/enable", active: true},
	} {
		response := browserAPIRequest(t, client, origin, step.method, step.path, step.body, true)
		var redirect db.Redirect
		decodeResponseJSON(t, response, &redirect)
		if response.StatusCode != http.StatusOK || (redirect.DisabledAt == nil) != step.active {
			t.Fatalf("%s status/redirect = %d/%#v", step.path, response.StatusCode, redirect)
		}
	}

	deleted := browserAPIRequest(t, client, origin, http.MethodDelete, "/api/v1/redirects/ui-link", "", true)
	if deleted.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status/body = %d/%s", deleted.StatusCode, readBody(t, deleted))
	}
	deleted.Body.Close()

	logout := browserAPIRequest(t, client, origin, http.MethodDelete, "/api/v1/session", "", true)
	if logout.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status/body = %d/%s", logout.StatusCode, readBody(t, logout))
	}
	logout.Body.Close()
	if len(jar.Cookies(parsedOrigin)) != 0 {
		t.Fatalf("cookie jar after logout = %#v", jar.Cookies(parsedOrigin))
	}
}

func TestCookieWritesRequireConfiguredOriginButBearerRemainsCompatible(t *testing.T) {
	database := testDatabase(t)
	const origin = "https://admin.example.com"
	router := routes.NewRouterWithAdmin(database, testManagementToken, "", routes.AdminConfig{
		Origin: origin, SessionSecret: testSessionSecret, SecureCookies: true,
	})
	login := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session", strings.NewReader(`{"token":"`+testManagementToken+`"}`))
	request.Header.Set("Origin", origin)
	router.ServeHTTP(login, request)
	cookie := login.Result().Cookies()[0]

	cookieWrite := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v1/redirects", strings.NewReader(`{"slug":"blocked","url":"https://example.com"}`))
	request.AddCookie(cookie)
	request.Header.Set("Origin", "https://evil.example.com")
	router.ServeHTTP(cookieWrite, request)
	if cookieWrite.Code != http.StatusForbidden {
		t.Fatalf("cross-origin cookie write status = %d, want 403", cookieWrite.Code)
	}

	bearerWrite := managementRequest(t, router, http.MethodPost, "/api/v1/redirects", `{"slug":"allowed","url":"https://example.com"}`, testManagementToken)
	if bearerWrite.Code != http.StatusCreated {
		t.Fatalf("bearer write without Origin status/body = %d/%s", bearerWrite.Code, bearerWrite.Body.String())
	}
}

func testDatabase(t *testing.T) *db.DbWrapper {
	t.Helper()
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	return database
}

func assertSecurityHeaders(t *testing.T, header http.Header) {
	t.Helper()
	if got := header.Get("Content-Security-Policy"); got != "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' blob:; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'" {
		t.Errorf("Content-Security-Policy = %q", got)
	}
	for name, want := range map[string]string{
		"X-Frame-Options":        "DENY",
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "no-referrer",
	} {
		if got := header.Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func browserAPIRequest(t *testing.T, client *http.Client, origin, method, path, body string, unsafe bool) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, origin+path, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if unsafe {
		request.Header.Set("Origin", origin)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if got := response.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("%s %s Cache-Control = %q, want no-store", method, path, got)
	}
	return response
}

func decodeResponseJSON(t *testing.T, response *http.Response, destination any) {
	t.Helper()
	defer response.Body.Close()
	if err := json.NewDecoder(response.Body).Decode(destination); err != nil {
		t.Fatal(err)
	}
}

func readBody(t *testing.T, response *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
