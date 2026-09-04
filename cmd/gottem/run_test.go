package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const testToken = "sentinel-management-token"

type recordedRequest struct {
	method      string
	requestURI  string
	authorize   string
	accept      string
	contentType string
	body        string
}

func TestCreateRequestsAndOutputs(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantBody string
		response string
		wantOut  string
	}{
		{
			name:     "custom slug",
			args:     []string{"create", "--slug", "launch", "https://example.com/page"},
			wantBody: `{"slug":"launch","url":"https://example.com/page"}`,
			response: redirectJSON("launch", "https://example.com/page", false),
			wantOut:  "Created http://example.test/launch -> https://example.com/page\n",
		},
		{
			name:     "generated slug",
			args:     []string{"create", "https://example.com/page"},
			wantBody: `{"url":"https://example.com/page"}`,
			response: redirectJSON("abc1234", "https://example.com/page", false),
			wantOut:  "Created http://example.test/abc1234 -> https://example.com/page\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got recordedRequest
			client := clientFor(func(request *http.Request) (*http.Response, error) {
				got = recordRequest(t, request)
				return jsonResponse(http.StatusCreated, test.response), nil
			})
			code, stdout, stderr := runCLI(t, append([]string{"--base-url", "http://example.test"}, test.args...), client, nil)
			if code != 0 || stdout != test.wantOut || stderr != "" {
				t.Fatalf("code/stdout/stderr = %d/%q/%q", code, stdout, stderr)
			}
			assertRequest(t, got, http.MethodPost, "/api/v1/redirects", test.wantBody, true)
		})
	}
}

func TestCommandHTTPMappingsAndJSON(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		method     string
		requestURI string
		body       string
		status     int
		response   string
		wantJSON   any
	}{
		{name: "list", args: []string{"list"}, method: http.MethodGet, requestURI: "/api/v1/redirects", status: http.StatusOK, response: "[" + redirectJSON("one", "https://example.com", false) + "]", wantJSON: []any{}},
		{name: "get escapes slug", args: []string{"get", "slash/space ?"}, method: http.MethodGet, requestURI: "/api/v1/redirects/slash%2Fspace%20%3F", status: http.StatusOK, response: redirectJSON("slash/space ?", "https://example.com", false), wantJSON: map[string]any{}},
		{name: "update", args: []string{"update", "known", "https://example.com/new"}, method: http.MethodPut, requestURI: "/api/v1/redirects/known", body: `{"url":"https://example.com/new"}`, status: http.StatusOK, response: redirectJSON("known", "https://example.com/new", false), wantJSON: map[string]any{}},
		{name: "disable", args: []string{"disable", "known"}, method: http.MethodPost, requestURI: "/api/v1/redirects/known/disable", status: http.StatusOK, response: redirectJSON("known", "https://example.com", true), wantJSON: map[string]any{}},
		{name: "delete", args: []string{"delete", "--force", "known"}, method: http.MethodDelete, requestURI: "/api/v1/redirects/known", status: http.StatusNoContent, wantJSON: map[string]any{"deleted": true, "slug": "known"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got recordedRequest
			client := clientFor(func(request *http.Request) (*http.Response, error) {
				got = recordRequest(t, request)
				return jsonResponse(test.status, test.response), nil
			})
			args := append([]string{"--base-url", "http://example.test/", "--json"}, test.args...)
			code, stdout, stderr := runCLI(t, args, client, nil)
			if code != 0 || stderr != "" {
				t.Fatalf("code/stderr = %d/%q", code, stderr)
			}
			var decoded any
			if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
				t.Fatalf("stdout is not JSON: %q: %v", stdout, err)
			}
			switch test.wantJSON.(type) {
			case []any:
				if _, ok := decoded.([]any); !ok {
					t.Fatalf("JSON = %#v, want array", decoded)
				}
			case map[string]any:
				object, ok := decoded.(map[string]any)
				if !ok {
					t.Fatalf("JSON = %#v, want object", decoded)
				}
				if test.name == "delete" && (object["deleted"] != true || object["slug"] != "known") {
					t.Fatalf("delete JSON = %#v", object)
				}
			}
			assertRequest(t, got, test.method, test.requestURI, test.body, test.body != "")
		})
	}
}

func TestEveryJSONSuccessContainsNoProse(t *testing.T) {
	tests := [][]string{
		{"create", "https://example.com"},
		{"list"},
		{"get", "known"},
		{"update", "known", "https://example.com/new"},
		{"disable", "known"},
		{"delete", "--force", "known"},
	}
	for _, command := range tests {
		t.Run(command[0], func(t *testing.T) {
			client := clientFor(func(request *http.Request) (*http.Response, error) {
				switch {
				case request.Method == http.MethodDelete:
					return jsonResponse(http.StatusNoContent, ""), nil
				case command[0] == "list":
					return jsonResponse(http.StatusOK, "[]"), nil
				case command[0] == "create":
					return jsonResponse(http.StatusCreated, redirectJSON("generated", "https://example.com", false)), nil
				default:
					return jsonResponse(http.StatusOK, redirectJSON("known", "https://example.com", command[0] == "disable")), nil
				}
			})
			code, stdout, stderr := runCLI(t, append([]string{"--base-url", "http://example.test", "--json"}, command...), client, nil)
			if code != 0 || stderr != "" || !json.Valid([]byte(stdout)) {
				t.Fatalf("code/stdout/stderr = %d/%q/%q", code, stdout, stderr)
			}
		})
	}
}

func TestHumanOutput(t *testing.T) {
	disabled := "2026-09-03T12:30:00Z"
	tests := []struct {
		name     string
		args     []string
		response string
		want     []string
	}{
		{name: "get active", args: []string{"get", "active"}, response: redirectJSON("active", "https://example.com/a", false), want: []string{"Slug: active", "URL: https://example.com/a", "Status: active", "Created: 2026-09-01T10:00:00Z", "Updated: 2026-09-02T11:00:00Z"}},
		{name: "get disabled", args: []string{"get", "off"}, response: redirectJSON("off", "https://example.com/o", true), want: []string{"Slug: off", "Status: disabled", "Disabled: " + disabled}},
		{name: "list active and disabled", args: []string{"list"}, response: "[" + redirectJSON("active", "https://example.com/a", false) + "," + redirectJSON("off", "https://example.com/o", true) + "]", want: []string{"SLUG", "STATUS", "DESTINATION", "active", "active", "https://example.com/a", "off", "disabled", "https://example.com/o"}},
		{name: "empty list", args: []string{"list"}, response: "[]", want: []string{"No redirects."}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := clientFor(func(request *http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusOK, test.response), nil
			})
			code, stdout, stderr := runCLI(t, append([]string{"--base-url", "http://example.test"}, test.args...), client, nil)
			if code != 0 || stderr != "" {
				t.Fatalf("code/stderr = %d/%q", code, stderr)
			}
			for _, want := range test.want {
				if !strings.Contains(stdout, want) {
					t.Errorf("stdout %q does not contain %q", stdout, want)
				}
			}
		})
	}
}

func TestHumanMutationOutput(t *testing.T) {
	tests := []struct {
		command  []string
		response string
		status   int
		want     string
	}{
		{command: []string{"update", "known", "https://example.com/new"}, response: redirectJSON("known", "https://example.com/new", false), status: http.StatusOK, want: "Updated known -> https://example.com/new\n"},
		{command: []string{"disable", "known"}, response: redirectJSON("known", "https://example.com", true), status: http.StatusOK, want: "Disabled known\n"},
		{command: []string{"delete", "--force", "known"}, status: http.StatusNoContent, want: "Deleted known\n"},
	}
	for _, test := range tests {
		t.Run(test.command[0], func(t *testing.T) {
			client := clientFor(func(request *http.Request) (*http.Response, error) {
				return jsonResponse(test.status, test.response), nil
			})
			code, stdout, stderr := runCLI(t, append([]string{"--base-url", "http://example.test"}, test.command...), client, nil)
			if code != 0 || stdout != test.want || stderr != "" {
				t.Fatalf("code/stdout/stderr = %d/%q/%q", code, stdout, stderr)
			}
		})
	}
}

func TestDeleteConfirmation(t *testing.T) {
	tests := []struct {
		name         string
		force        bool
		confirmed    bool
		confirmErr   error
		wantCode     int
		wantRequests int
	}{
		{name: "yes", confirmed: true, wantCode: 0, wantRequests: 1},
		{name: "no", confirmed: false, wantCode: 1},
		{name: "error", confirmErr: errors.New("not a terminal"), wantCode: 1},
		{name: "force bypasses", force: true, confirmErr: errors.New("must not be called"), wantCode: 0, wantRequests: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requests, confirms := 0, 0
			client := clientFor(func(request *http.Request) (*http.Response, error) {
				requests++
				return jsonResponse(http.StatusNoContent, ""), nil
			})
			confirm := func(slug string) (bool, error) {
				confirms++
				if slug != "known" {
					t.Fatalf("confirm slug = %q", slug)
				}
				return test.confirmed, test.confirmErr
			}
			args := []string{"--base-url", "http://example.test", "delete"}
			if test.force {
				args = append(args, "--force")
			}
			args = append(args, "known")
			code, _, _ := runCLI(t, args, client, confirm)
			if code != test.wantCode || requests != test.wantRequests {
				t.Fatalf("code/requests = %d/%d, want %d/%d", code, requests, test.wantCode, test.wantRequests)
			}
			if test.force && confirms != 0 {
				t.Fatalf("force called confirm %d times", confirms)
			}
		})
	}
}

func TestDefaultConfirm(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{input: "y\n", want: true},
		{input: "YES\n", want: true},
		{input: "n\n", want: false},
		{input: "\n", want: false},
		{input: "anything\n", want: false},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%q", test.input), func(t *testing.T) {
			var prompt bytes.Buffer
			got, err := promptConfirm(strings.NewReader(test.input), &prompt, "known")
			if err != nil || got != test.want {
				t.Fatalf("confirm = %v, %v; want %v", got, err, test.want)
			}
			if prompt.String() != `Delete redirect "known" permanently? [y/N] ` {
				t.Fatalf("prompt = %q", prompt.String())
			}
		})
	}
}

func TestNonTTYConfirmationRefuses(t *testing.T) {
	var stderr bytes.Buffer
	confirmed, err := terminalConfirm(strings.NewReader("yes\n"), &stderr, "known")
	if confirmed || err == nil || err.Error() != "delete requires an interactive terminal; use --force" {
		t.Fatalf("confirmed/error = %v/%v", confirmed, err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestMissingTokenFailsBeforeRequest(t *testing.T) {
	t.Setenv("GOTTEM_MANAGEMENT_TOKEN", "")
	requests := 0
	client := clientFor(func(request *http.Request) (*http.Response, error) {
		requests++
		return nil, errors.New("unexpected request")
	})
	var stdout, stderr bytes.Buffer
	code := run([]string{"list"}, strings.NewReader(""), &stdout, &stderr, client, nil)
	if code != 2 || requests != 0 || !strings.Contains(stderr.String(), "GOTTEM_MANAGEMENT_TOKEN") {
		t.Fatalf("code/requests/stderr = %d/%d/%q", code, requests, stderr.String())
	}
}

func TestTokenNeverAppearsInDiagnostics(t *testing.T) {
	tests := []struct {
		name   string
		client *http.Client
	}{
		{name: "401 echoes token", client: clientFor(func(request *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusUnauthorized, `{"error":"bad `+testToken+`"}`), nil
		})},
		{name: "500 body token", client: clientFor(func(request *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusInternalServerError, testToken+"\ncontrol\x00"), nil
		})},
		{name: "transport error token", client: clientFor(func(request *http.Request) (*http.Response, error) {
			return nil, errors.New("failed with " + testToken)
		})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			code, stdout, stderr := runCLI(t, []string{"--base-url", "http://example.test", "list"}, test.client, nil)
			if code != 1 || stdout != "" {
				t.Fatalf("code/stdout = %d/%q", code, stdout)
			}
			if strings.Contains(stderr, testToken) {
				t.Fatalf("diagnostic leaked token: %q", stderr)
			}
			for _, character := range stderr {
				if character < 0x20 && character != '\n' && character != '\t' {
					t.Fatalf("diagnostic contains control character: %q", stderr)
				}
			}
		})
	}
}

func TestTokenIsRedactedBeforeErrorExcerptIsTruncated(t *testing.T) {
	body := strings.Repeat("x", 195) + testToken + strings.Repeat("y", 20)
	client := clientFor(func(request *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusInternalServerError, body), nil
	})
	code, _, stderr := runCLI(t, []string{"--base-url", "http://example.test", "list"}, client, nil)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if strings.Contains(stderr, testToken[:5]) {
		t.Fatalf("diagnostic leaked token prefix: %q", stderr)
	}
}

func TestAPIErrorIncludesDecodedField(t *testing.T) {
	client := clientFor(func(request *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusBadRequest, `{"error":"invalid slug","field":"slug"}`), nil
	})
	code, _, stderr := runCLI(t, []string{"--base-url", "http://example.test", "create", "--slug", "bad", "https://example.com"}, client, nil)
	if code != 1 || !strings.Contains(stderr, "invalid slug") || !strings.Contains(stderr, "slug") {
		t.Fatalf("code/stderr = %d/%q", code, stderr)
	}
}

func TestOutOfRangeBaseURLPortFailsBeforeRequest(t *testing.T) {
	requests := 0
	client := clientFor(func(request *http.Request) (*http.Response, error) {
		requests++
		return jsonResponse(http.StatusOK, "[]"), nil
	})

	code, stdout, stderr := runCLI(t, []string{"--base-url", "http://example.com:99999", "list"}, client, nil)
	if code != 2 || stdout != "" || requests != 0 || !strings.Contains(stderr, "invalid base URL") {
		t.Fatalf("code/stdout/stderr/requests = %d/%q/%q/%d", code, stdout, stderr, requests)
	}
}

func TestUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "empty", args: nil},
		{name: "unknown command", args: []string{"wat"}},
		{name: "missing create URL", args: []string{"create"}},
		{name: "empty create slug", args: []string{"create", "--slug", "", "https://example.com"}},
		{name: "extra create arg", args: []string{"create", "https://example.com", "extra"}},
		{name: "misplaced global flag", args: []string{"list", "--json"}},
		{name: "unknown global flag", args: []string{"--wat", "list"}},
		{name: "get missing slug", args: []string{"get"}},
		{name: "get extra arg", args: []string{"get", "one", "two"}},
		{name: "update missing URL", args: []string{"update", "one"}},
		{name: "disable empty slug", args: []string{"disable", ""}},
		{name: "delete missing slug", args: []string{"delete", "--force"}},
		{name: "create misplaced slug flag", args: []string{"create", "https://example.com", "--slug", "known"}},
		{name: "delete misplaced force flag", args: []string{"delete", "known", "--force"}},
		{name: "invalid base relative", args: []string{"--base-url", "/local", "list"}},
		{name: "invalid base scheme", args: []string{"--base-url", "ftp://example.com", "list"}},
		{name: "invalid base userinfo", args: []string{"--base-url", "https://user@example.com", "list"}},
		{name: "invalid base query", args: []string{"--base-url", "https://example.com?q=1", "list"}},
		{name: "invalid base fragment", args: []string{"--base-url", "https://example.com#x", "list"}},
		{name: "invalid base path", args: []string{"--base-url", "https://example.com/api", "list"}},
		{name: "empty base", args: []string{"--base-url", "", "list"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			client := clientFor(func(request *http.Request) (*http.Response, error) {
				requests++
				return jsonResponse(http.StatusOK, "[]"), nil
			})
			code, stdout, stderr := runCLI(t, test.args, client, nil)
			if code != 2 || stdout != "" || requests != 0 || stderr == "" {
				t.Fatalf("code/stdout/stderr/requests = %d/%q/%q/%d", code, stdout, stderr, requests)
			}
		})
	}
}

func TestBaseURLFromEnvironmentAndDefault(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		args    []string
		wantURL string
	}{
		{name: "environment", env: "http://environment.test/", args: []string{"list"}, wantURL: "http://environment.test/api/v1/redirects"},
		{name: "flag overrides environment", env: "http://environment.test", args: []string{"--base-url", "http://flag.test/", "list"}, wantURL: "http://flag.test/api/v1/redirects"},
		{name: "default", env: "", args: []string{"list"}, wantURL: "https://gottem.link/api/v1/redirects"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("GOTTEM_BASE_URL", test.env)
			var got string
			client := clientFor(func(request *http.Request) (*http.Response, error) {
				got = request.URL.String()
				return jsonResponse(http.StatusOK, "[]"), nil
			})
			code, _, stderr := runCLI(t, test.args, client, nil)
			if code != 0 || stderr != "" || got != test.wantURL {
				t.Fatalf("code/stderr/url = %d/%q/%q, want %q", code, stderr, got, test.wantURL)
			}
		})
	}
}

func TestMalformedOversizedAndConnectionFailures(t *testing.T) {
	tests := []struct {
		name   string
		client *http.Client
	}{
		{name: "malformed success JSON", client: clientFor(func(request *http.Request) (*http.Response, error) { return jsonResponse(http.StatusOK, `{`), nil })},
		{name: "oversized success response", client: clientFor(func(request *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{"padding":"`+strings.Repeat("x", maxResponseBodyBytes)+`"}`), nil
		})},
		{name: "connection failure", client: clientFor(func(request *http.Request) (*http.Response, error) { return nil, io.ErrUnexpectedEOF })},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			code, stdout, stderr := runCLI(t, []string{"--base-url", "http://example.test", "list"}, test.client, nil)
			if code != 1 || stdout != "" || stderr == "" {
				t.Fatalf("code/stdout/stderr = %d/%q/%q", code, stdout, stderr)
			}
		})
	}
}

func TestCommandsRejectUnexpectedSuccessStatuses(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		status   int
		response string
	}{
		{name: "create wants 201", args: []string{"create", "https://example.com"}, status: http.StatusOK, response: redirectJSON("known", "https://example.com", false)},
		{name: "list wants 200", args: []string{"list"}, status: http.StatusCreated, response: "[]"},
		{name: "delete wants 204", args: []string{"delete", "--force", "known"}, status: http.StatusOK, response: `{}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := clientFor(func(request *http.Request) (*http.Response, error) {
				return jsonResponse(test.status, test.response), nil
			})
			code, stdout, stderr := runCLI(t, append([]string{"--base-url", "http://example.test"}, test.args...), client, nil)
			if code != 1 || stdout != "" || stderr == "" {
				t.Fatalf("code/stdout/stderr = %d/%q/%q", code, stdout, stderr)
			}
		})
	}
}

func TestClientDoesNotFollowRedirects(t *testing.T) {
	landingRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/landing" {
			landingRequests++
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte("[]"))
			return
		}
		http.Redirect(writer, request, "/landing", http.StatusFound)
	}))
	t.Cleanup(server.Close)

	code, stdout, stderr := runCLI(t, []string{"--base-url", server.URL, "list"}, server.Client(), nil)
	if code != 1 || stdout != "" || stderr == "" {
		t.Fatalf("code/stdout/stderr = %d/%q/%q", code, stdout, stderr)
	}
	if landingRequests != 0 {
		t.Fatalf("followed redirect with %d landing requests", landingRequests)
	}
}

func TestDefaultHTTPTimeout(t *testing.T) {
	if defaultHTTPTimeout != 10*time.Second {
		t.Fatalf("defaultHTTPTimeout = %v, want 10s", defaultHTTPTimeout)
	}
}

func runCLI(t *testing.T, args []string, client *http.Client, confirm confirmFunc) (int, string, string) {
	t.Helper()
	t.Setenv("GOTTEM_MANAGEMENT_TOKEN", testToken)
	var stdout, stderr bytes.Buffer
	code := run(args, strings.NewReader(""), &stdout, &stderr, client, confirm)
	return code, stdout.String(), stderr.String()
}

func clientFor(roundTrip func(*http.Request) (*http.Response, error)) *http.Client {
	return &http.Client{Transport: roundTripper(roundTrip)}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (roundTrip roundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func recordRequest(t *testing.T, request *http.Request) recordedRequest {
	t.Helper()
	var body []byte
	var err error
	if request.Body != nil {
		body, err = io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
	}
	return recordedRequest{
		method:      request.Method,
		requestURI:  request.URL.EscapedPath(),
		authorize:   request.Header.Get("Authorization"),
		accept:      request.Header.Get("Accept"),
		contentType: request.Header.Get("Content-Type"),
		body:        string(body),
	}
}

func assertRequest(t *testing.T, got recordedRequest, method, requestURI, body string, hasBody bool) {
	t.Helper()
	if got.method != method || got.requestURI != requestURI || got.authorize != "Bearer "+testToken || got.accept != "application/json" || got.body != body {
		t.Fatalf("request = %#v, want method=%s uri=%s body=%q", got, method, requestURI, body)
	}
	if hasBody && got.contentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got.contentType)
	}
	if !hasBody && got.contentType != "" {
		t.Errorf("Content-Type = %q, want empty", got.contentType)
	}
}

func redirectJSON(slug, destination string, disabled bool) string {
	disabledJSON := "null"
	if disabled {
		disabledJSON = `"2026-09-03T12:30:00Z"`
	}
	slugJSON, _ := json.Marshal(slug)
	urlJSON, _ := json.Marshal(destination)
	return fmt.Sprintf(`{"id":1,"slug":%s,"url":%s,"created_at":"2026-09-01T10:00:00Z","updated_at":"2026-09-02T11:00:00Z","disabled_at":%s}`, slugJSON, urlJSON, disabledJSON)
}

func TestParseBaseURLRejectsEncodedPath(t *testing.T) {
	for _, raw := range []string{"https://example.com/%2F", "https://example.com//"} {
		if _, err := parseBaseURL(raw); err == nil {
			t.Errorf("parseBaseURL(%q) succeeded", raw)
		}
	}
}

func TestParseBaseURLNormalizesScheme(t *testing.T) {
	parsed, err := parseBaseURL("HTTPS://example.com")
	if err != nil {
		t.Fatalf("parse uppercase scheme: %v", err)
	}
	if parsed.Scheme != "https" {
		t.Fatalf("scheme = %q, want https", parsed.Scheme)
	}
}

func TestRequestURLDoesNotNormalizeEscapedSlug(t *testing.T) {
	base, err := url.Parse("https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	got := redirectURL(base, "a/b")
	if got.EscapedPath() != "/api/v1/redirects/a%2Fb" {
		t.Fatalf("escaped path = %q", got.EscapedPath())
	}
}
