package aap

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"

	"aaptui/internal/config"
)

// CreateToken creates one personal access token in the configured mode.
// It never retries or follows redirects, including with an injected HTTP client.
func CreateToken(ctx context.Context, s config.Settings, username, password string, injected *http.Client) (token string, resultErr error) {
	const op = "create API token"
	if err := config.Validate(s); err != nil {
		return "", apiError(Malformed, op)
	}
	if strings.TrimSpace(username) == "" || len(username) > 4096 || strings.Contains(username, ":") || password == "" || len(password) > 4096 || strings.ContainsFunc(username+password, unicode.IsControl) {
		return "", apiError(Authentication, op)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	path := "/api/gateway/v1/tokens/"
	payload := `{"description":"aaptui","scope":"write"}`
	if s.Mode == config.Direct {
		path = "/api/v2/tokens/"
		payload = `{"description":"aaptui","scope":"write","application":null}`
	}
	endpoint := strings.TrimRight(s.URL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(payload))
	if err != nil {
		return "", apiError(Malformed, op)
	}
	req.SetBasicAuth(username, password)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	uncertain := func(kind ErrorKind) error {
		return fmt.Errorf("%w; result uncertain, check server tokens before trying again", apiError(kind, op))
	}
	resp, err := connectionHTTPClient(s, injected).Do(req)
	if err != nil {
		return "", uncertain(Connection)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil && resultErr == nil {
			token, resultErr = "", uncertain(Connection)
		}
	}()
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
	case http.StatusUnauthorized:
		return "", fmt.Errorf("%w; check login credentials or supply an existing AAP_TOKEN", apiError(Authentication, op))
	case http.StatusForbidden:
		return "", fmt.Errorf("%w; token creation may be disabled for this account; supply an existing AAP_TOKEN", apiError(Permission, op))
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return "", fmt.Errorf("%w; token creation is unavailable for this connection; check the connection mode or supply AAP_TOKEN", apiError(Unsupported, op))
	case http.StatusBadRequest:
		return "", apiError(Rejected, op)
	default:
		return "", uncertain(Temporary)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024+1))
	var response struct {
		Token string `json:"token"`
	}
	if err != nil || len(data) > 64*1024 || json.Unmarshal(data, &response) != nil || config.ValidateToken(response.Token) != nil {
		return "", uncertain(Malformed)
	}
	return response.Token, nil
}
