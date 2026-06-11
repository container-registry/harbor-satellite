package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// NewClient creates an API client for the given server URL and token.
// Callers must invoke ValidateScheme before constructing a client that will
// send credentials, to ensure the URL uses HTTPS outside of localhost.
func NewClient(baseURL, token string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		HTTP: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ValidateScheme returns an error if baseURL uses a non-HTTPS scheme against
// a non-localhost host. Call this before sending credentials.
func ValidateScheme(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid server URL: %w", err)
	}
	host := u.Hostname()
	isLocal := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if !isLocal && u.Scheme != "https" {
		return fmt.Errorf("refusing to send credentials over %s; use https:// or a localhost address", u.Scheme)
	}
	return nil
}

type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API error (HTTP %d): %s", e.StatusCode, e.Message)
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

type WhoamiResponse struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	Server   string `json:"server,omitempty"`
}

func (c *Client) Login(ctx context.Context, username, password string) (*LoginResponse, error) {
	payload := LoginRequest{Username: username, Password: password}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal login request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/login", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, parseAPIError(resp)
	}

	var result LoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode login response: %w", err)
	}
	return &result, nil
}

func (c *Client) Logout(ctx context.Context) error {
	req, err := c.newAuthRequest(ctx, http.MethodPost, "/api/logout", nil)
	if err != nil {
		return err
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return parseAPIError(resp)
	}
	return nil
}

func (c *Client) Whoami(ctx context.Context) (*WhoamiResponse, error) {
	req, err := c.newAuthRequest(ctx, http.MethodGet, "/api/whoami", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, parseAPIError(resp)
	}

	var result WhoamiResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode whoami response: %w", err)
	}
	return &result, nil
}

func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/ping", nil)
	if err != nil {
		return fmt.Errorf("failed to create ping request: %w", err)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("server unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) newAuthRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	return req, nil
}

func parseAPIError(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &APIError{StatusCode: resp.StatusCode, Message: fmt.Sprintf("(failed to read error body: %v)", err)}
	}

	var errResp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error != "" {
		return &APIError{StatusCode: resp.StatusCode, Message: errResp.Error}
	}

	return &APIError{StatusCode: resp.StatusCode, Message: string(body)}
}
