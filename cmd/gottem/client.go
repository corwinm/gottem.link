package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode"

	"corwinm/gottem.link/backup"
)

const maxResponseBodyBytes = 1 << 20

type redirect struct {
	ID         int64   `json:"id"`
	Slug       string  `json:"slug"`
	URL        string  `json:"url"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
	DisabledAt *string `json:"disabled_at"`
}

type apiError struct {
	Error     string   `json:"error"`
	Field     string   `json:"field"`
	Conflicts []string `json:"conflicts"`
}

type importResult struct {
	Total    int `json:"total"`
	Imported int `json:"imported"`
}

type managementClient struct {
	baseURL *url.URL
	token   string
	http    *http.Client
}

func (client managementClient) create(ctx context.Context, slug, destination string) (redirect, error) {
	payload := struct {
		Slug *string `json:"slug,omitempty"`
		URL  string  `json:"url"`
	}{URL: destination}
	if slug != "" {
		payload.Slug = &slug
	}
	var result redirect
	err := client.requestJSON(ctx, http.MethodPost, collectionURL(client.baseURL), payload, http.StatusCreated, &result)
	return result, err
}

func (client managementClient) list(ctx context.Context) ([]redirect, error) {
	result := make([]redirect, 0)
	err := client.requestJSON(ctx, http.MethodGet, collectionURL(client.baseURL), nil, http.StatusOK, &result)
	return result, err
}

func (client managementClient) export(ctx context.Context) (backup.Envelope, error) {
	var raw json.RawMessage
	if err := client.requestJSON(ctx, http.MethodGet, exportURL(client.baseURL), nil, http.StatusOK, &raw); err != nil {
		return backup.Envelope{}, err
	}
	result, err := backup.Decode(bytes.NewReader(raw))
	if err != nil {
		return backup.Envelope{}, errors.New("server returned invalid export")
	}
	return result, nil
}

func (client managementClient) get(ctx context.Context, slug string) (redirect, error) {
	var result redirect
	err := client.requestJSON(ctx, http.MethodGet, redirectURL(client.baseURL, slug), nil, http.StatusOK, &result)
	return result, err
}

func (client managementClient) update(ctx context.Context, slug, destination string) (redirect, error) {
	var result redirect
	err := client.requestJSON(ctx, http.MethodPut, redirectURL(client.baseURL, slug), struct {
		URL string `json:"url"`
	}{URL: destination}, http.StatusOK, &result)
	return result, err
}

func (client managementClient) disable(ctx context.Context, slug string) (redirect, error) {
	var result redirect
	err := client.requestJSON(ctx, http.MethodPost, disableURL(client.baseURL, slug), nil, http.StatusOK, &result)
	return result, err
}

func (client managementClient) delete(ctx context.Context, slug string) error {
	return client.requestJSON(ctx, http.MethodDelete, redirectURL(client.baseURL, slug), nil, http.StatusNoContent, nil)
}

func (client managementClient) importRedirects(ctx context.Context, envelope backup.Envelope, dryRun bool) (importResult, error) {
	var result importResult
	err := client.requestJSON(ctx, http.MethodPost, importURL(client.baseURL, dryRun), envelope, http.StatusOK, &result)
	return result, err
}

func (client managementClient) requestJSON(ctx context.Context, method string, target *url.URL, payload any, expectedStatus int, result any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	httpClient := *client.http
	httpClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := readBounded(response.Body)
	if err != nil {
		return err
	}
	if response.StatusCode != expectedStatus {
		return responseError(response, responseBody, client.token)
	}
	if result == nil {
		return nil
	}
	if len(responseBody) == 0 {
		return errors.New("server returned an empty success response")
	}
	if err := json.Unmarshal(responseBody, result); err != nil {
		return errors.New("server returned malformed JSON")
	}
	return nil
}

func readBounded(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maxResponseBodyBytes+1))
	if err != nil {
		return nil, errors.New("read response failed")
	}
	if len(body) > maxResponseBodyBytes {
		return nil, errors.New("server response exceeds 1 MiB")
	}
	return body, nil
}

func responseError(response *http.Response, body []byte, token string) error {
	status := response.Status
	if status == "" {
		status = fmt.Sprintf("%d %s", response.StatusCode, http.StatusText(response.StatusCode))
	}
	var payload apiError
	if json.Unmarshal(body, &payload) == nil && payload.Error != "" {
		if len(payload.Conflicts) > 0 {
			return fmt.Errorf("request failed: %s: %s: %s", status, payload.Error, strings.Join(payload.Conflicts, ", "))
		}
		if payload.Field != "" {
			return fmt.Errorf("request failed: %s: %s (field: %s)", status, payload.Error, payload.Field)
		}
		return fmt.Errorf("request failed: %s: %s", status, payload.Error)
	}
	excerpt := sanitize(string(body))
	if token != "" {
		excerpt = strings.ReplaceAll(excerpt, token, "[redacted]")
	}
	if len(excerpt) > 200 {
		excerpt = excerpt[:200] + "..."
	}
	if excerpt == "" {
		return fmt.Errorf("request failed: %s", status)
	}
	return fmt.Errorf("request failed: %s: %s", status, excerpt)
}

func sanitize(value string) string {
	return strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return ' '
		}
		return character
	}, value)
}

func collectionURL(base *url.URL) *url.URL {
	result := *base
	result.Path = "/api/v1/redirects"
	return &result
}

func exportURL(base *url.URL) *url.URL {
	result := *base
	result.Path = "/api/v1/exports"
	return &result
}

func redirectURL(base *url.URL, slug string) *url.URL {
	result := *base
	result.Path = "/api/v1/redirects/" + slug
	result.RawPath = "/api/v1/redirects/" + url.PathEscape(slug)
	return &result
}

func disableURL(base *url.URL, slug string) *url.URL {
	result := redirectURL(base, slug)
	result.Path += "/disable"
	result.RawPath += "/disable"
	return result
}

func importURL(base *url.URL, dryRun bool) *url.URL {
	result := *base
	result.Path = "/api/v1/imports"
	if dryRun {
		result.RawQuery = "dry_run=true"
	}
	return &result
}
