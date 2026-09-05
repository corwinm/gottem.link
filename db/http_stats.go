package db

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const accessRequestTimeout = 500 * time.Millisecond
const internalAccessPath = "/.internal/accesses"

type HTTPAccessStore struct {
	url    string
	token  string
	client *http.Client
}

type accessRequest struct {
	RedirectID int64  `json:"redirect_id"`
	AccessedAt string `json:"accessed_at"`
}

func NewHTTPAccessStore(proxyURL, token string, client *http.Client) (*HTTPAccessStore, error) {
	if token == "" {
		return nil, errors.New("access store token is required")
	}
	parsed, err := url.Parse(proxyURL)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("access store proxy URL must be a loopback HTTP origin")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return nil, errors.New("access store proxy URL must not include a path")
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
		return nil, errors.New("access store proxy URL must use a loopback host")
	}
	parsed.Path = internalAccessPath

	if client == nil {
		client = http.DefaultClient
	}
	boundedClient := *client
	if boundedClient.Timeout <= 0 || boundedClient.Timeout > accessRequestTimeout {
		boundedClient.Timeout = accessRequestTimeout
	}
	return &HTTPAccessStore{url: parsed.String(), token: token, client: &boundedClient}, nil
}

func (store *HTTPAccessStore) RecordRedirectAccess(ctx context.Context, id int64, accessedAt time.Time) error {
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(accessRequest{RedirectID: id, AccessedAt: accessedAt.UTC().Format(time.RFC3339Nano)}); err != nil {
		return fmt.Errorf("encode access: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, store.url, &body)
	if err != nil {
		return fmt.Errorf("create access request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+store.token)
	request.Header.Set("Content-Type", "application/json")
	response, err := store.client.Do(request)
	if err != nil {
		return fmt.Errorf("send access request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("send access request: unexpected HTTP status %d", response.StatusCode)
	}
	return nil
}
