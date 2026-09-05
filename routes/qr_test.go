package routes_test

import (
	"bytes"
	"image/color"
	"image/png"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"

	"corwinm/gottem.link/db"
	"corwinm/gottem.link/routes"
	go_qr "github.com/piglig/go-qr"
)

const testAdminOrigin = "https://admin.example.com"

func TestQRCodeEndpointRequiresFullyConfiguredAdmin(t *testing.T) {
	database := testDatabase(t)
	if _, err := database.CreateRedirect("known", "https://destination.example/private"); err != nil {
		t.Fatal(err)
	}

	configs := []routes.AdminConfig{
		{},
		{Origin: testAdminOrigin, SessionSecret: testSessionSecret},
		{Origin: "", SessionSecret: testSessionSecret, SecureCookies: true},
	}
	for _, config := range configs {
		response := managementRequest(t, routes.NewRouterWithAdmin(database, testManagementToken, "", config), http.MethodGet, "/api/v1/redirects/known/qr.png", "", testManagementToken)
		if response.Code != http.StatusNotFound {
			t.Fatalf("config %#v status = %d, want 404", config, response.Code)
		}
	}
}

func TestQRCodeEndpointAuthenticationAndMethods(t *testing.T) {
	database := testDatabase(t)
	if _, err := database.CreateRedirect("known", "https://destination.example/private"); err != nil {
		t.Fatal(err)
	}
	router := qrRouter(database)

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(method, "/api/v1/redirects/known/qr.png", nil))
		if response.Code != http.StatusUnauthorized {
			t.Errorf("unauthenticated %s status = %d, want 401", method, response.Code)
		}
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("unauthenticated %s Cache-Control = %q", method, response.Header().Get("Cache-Control"))
		}
	}

	response := managementRequest(t, router, http.MethodPost, "/api/v1/redirects/known/qr.png", "", testManagementToken)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status/body = %d/%q, want 405", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Allow"); got != "GET, HEAD" {
		t.Errorf("Allow = %q, want GET, HEAD", got)
	}
}

func TestQRCodeEndpointEncodesCanonicalShortURLForEveryExistingState(t *testing.T) {
	database := testDatabase(t)
	active, err := database.CreateRedirect("active-link", "https://destination.example/secret?token=not-for-qr")
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := database.CreateRedirect("disabled-link", "https://destination.example/disabled")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.DisableRedirect(disabled.Slug); err != nil {
		t.Fatal(err)
	}
	past := "2000-01-01T00:00:00Z"
	expired, err := database.CreateRedirectWithExpiration("expired-link", "https://destination.example/expired", &past)
	if err != nil {
		t.Fatal(err)
	}
	router := qrRouter(database)

	for _, redirect := range []db.Redirect{active, disabled, expired} {
		t.Run(redirect.Slug, func(t *testing.T) {
			response := managementRequest(t, router, http.MethodGet, "/api/v1/redirects/"+redirect.Slug+"/qr.png", "", testManagementToken)
			if response.Code != http.StatusOK {
				t.Fatalf("status/body = %d/%q", response.Code, response.Body.String())
			}
			assertQRCodeResponse(t, response, testAdminOrigin+"/"+url.PathEscape(redirect.Slug))
		})
	}
}

func TestQRCodeEndpointUsesStoredSlugForExactEscapingAndIsDeterministic(t *testing.T) {
	database := testDatabase(t)
	if _, err := database.CreateRedirect("mixed-case-request", "https://destination.example"); err != nil {
		t.Fatal(err)
	}
	router := qrRouter(database)
	path := "/api/v1/redirects/MIXED-CASE-REQUEST/qr.png"

	first := managementRequest(t, router, http.MethodGet, path, "", testManagementToken)
	second := managementRequest(t, router, http.MethodGet, path, "", testManagementToken)
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("statuses = %d/%d", first.Code, second.Code)
	}
	if !bytes.Equal(first.Body.Bytes(), second.Body.Bytes()) {
		t.Fatal("same short URL produced different PNG bytes")
	}
	assertQRCodeResponse(t, first, testAdminOrigin+"/mixed-case-request")
}

func TestQRCodeEndpointHEADMatchesGETWithoutBody(t *testing.T) {
	database := testDatabase(t)
	if _, err := database.CreateRedirect("known", "https://destination.example"); err != nil {
		t.Fatal(err)
	}
	router := qrRouter(database)
	get := managementRequest(t, router, http.MethodGet, "/api/v1/redirects/known/qr.png", "", testManagementToken)
	head := managementRequest(t, router, http.MethodHead, "/api/v1/redirects/known/qr.png", "", testManagementToken)
	if get.Code != http.StatusOK || head.Code != http.StatusOK {
		t.Fatalf("GET/HEAD status = %d/%d", get.Code, head.Code)
	}
	if head.Body.Len() != 0 {
		t.Fatalf("HEAD body length = %d, want 0", head.Body.Len())
	}
	for _, name := range []string{"Content-Type", "Content-Length", "Cache-Control", "X-Content-Type-Options", "Content-Security-Policy"} {
		if get.Header().Get(name) == "" || head.Header().Get(name) != get.Header().Get(name) {
			t.Errorf("%s GET/HEAD = %q/%q", name, get.Header().Get(name), head.Header().Get(name))
		}
	}
}

func TestQRCodeEndpointMissingAndDatabaseFailure(t *testing.T) {
	database := testDatabase(t)
	router := qrRouter(database)
	missing := managementRequest(t, router, http.MethodGet, "/api/v1/redirects/missing/qr.png", "", testManagementToken)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing status/body = %d/%q", missing.Code, missing.Body.String())
	}
	if missing.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("missing Cache-Control = %q", missing.Header().Get("Cache-Control"))
	}

	database.Close()
	failed := managementRequest(t, router, http.MethodGet, "/api/v1/redirects/missing/qr.png", "", testManagementToken)
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("database failure status/body = %d/%q", failed.Code, failed.Body.String())
	}
}

func TestQRCodeEndpointAcceptsBrowserSessionCookie(t *testing.T) {
	database := testDatabase(t)
	if _, err := database.CreateRedirect("cookie-link", "https://destination.example"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(nil)
	origin := "http://" + server.Listener.Addr().String()
	server.Config.Handler = routes.NewRouterWithAdmin(database, testManagementToken, "", routes.AdminConfig{
		Origin: origin, SessionSecret: testSessionSecret, SecureCookies: false,
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
	login.Body.Close()

	response, err := client.Get(origin + "/api/v1/redirects/cookie-link/qr.png")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("cookie QR status = %d", response.StatusCode)
	}
	image, err := png.Decode(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := go_qr.Decode(image)
	if err != nil || decoded != origin+"/cookie-link" {
		t.Fatalf("decoded = %q, err = %v", decoded, err)
	}
}

func TestQRCodeRouteDoesNotChangePublicRedirect(t *testing.T) {
	database := testDatabase(t)
	if _, err := database.CreateRedirect("known", "https://destination.example/public"); err != nil {
		t.Fatal(err)
	}
	router := qrRouter(database)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/known", nil))
	if response.Code != http.StatusFound || response.Header().Get("Location") != "https://destination.example/public" {
		t.Fatalf("public redirect = %d/%q", response.Code, response.Header().Get("Location"))
	}
}

func qrRouter(database *db.DbWrapper) http.Handler {
	return routes.NewRouterWithAdmin(database, testManagementToken, "", routes.AdminConfig{
		Origin: testAdminOrigin, SessionSecret: testSessionSecret, SecureCookies: true,
	})
}

func assertQRCodeResponse(t *testing.T, response *httptest.ResponseRecorder, want string) {
	t.Helper()
	if got := response.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", got)
	}
	for name, value := range map[string]string{
		"Cache-Control":           "no-store",
		"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'; sandbox",
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "no-referrer",
	} {
		if got := response.Header().Get(name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
	body := response.Body.Bytes()
	if len(body) < 8 || !bytes.Equal(body[:8], []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("body does not have PNG signature: %x", body[:min(len(body), 8)])
	}
	image, err := png.Decode(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("decode PNG: %v", err)
	}
	result, err := go_qr.DecodeDetailed(image)
	if err != nil {
		t.Fatalf("decode QR: %v", err)
	}
	if result.Text != want {
		t.Errorf("decoded payload = %q, want %q", result.Text, want)
	}
	if result.Ecc != go_qr.High {
		t.Errorf("ECC = %v, want High", result.Ecc)
	}
	bounds := image.Bounds()
	wantDimension := (17 + 4*result.Version + 8) * 8
	if bounds.Dx() != wantDimension || bounds.Dy() != wantDimension {
		t.Errorf("dimensions = %dx%d, want %dx%d", bounds.Dx(), bounds.Dy(), wantDimension, wantDimension)
	}
	if got := response.Header().Get("Content-Length"); got == "" {
		t.Error("Content-Length is missing")
	}
	quietPixels := 4 * 8
	white := color.RGBA{255, 255, 255, 255}
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			if x >= quietPixels && x < bounds.Dx()-quietPixels && y >= quietPixels && y < bounds.Dy()-quietPixels {
				continue
			}
			if got := color.RGBAModel.Convert(image.At(x, y)).(color.RGBA); got != white {
				t.Fatalf("quiet zone pixel (%d,%d) = %#v, want white", x, y, got)
			}
		}
	}
}
