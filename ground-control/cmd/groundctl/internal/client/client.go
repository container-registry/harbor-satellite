package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const configDir = ".groundctl"

// Satellite represents a satellite as returned by the Ground Control API.
// Field names match the PascalCase output of Go's json.Marshal on the
// sqlc-generated database.Satellite struct (which has no json tags).
type Satellite struct {
	ID                int32      `json:"ID"`
	Name              string     `json:"Name"`
	CreatedAt         time.Time  `json:"CreatedAt"`
	UpdatedAt         time.Time  `json:"UpdatedAt"`
	LastSeen          *time.Time `json:"LastSeen"`
	HeartbeatInterval *string    `json:"HeartbeatInterval"`
}

// LoginRequest is the payload for POST /login.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse is the response from POST /login.
type LoginResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

// ErrorResponse represents an API error.
type ErrorResponse struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

// Client is an HTTP client for the Ground Control API.
type Client struct {
	baseURL    string
	httpClient *http.Client
	token      string
}

// NewClient creates a new Client for the given Ground Control server URL.
func NewClient(serverURL string) *Client {
	return &Client{
		baseURL:    serverURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// SetServer updates the Ground Control server URL.
func (c *Client) SetServer(serverURL string) {
	c.baseURL = serverURL
}

// Server returns the current Ground Control server URL.
func (c *Client) Server() string {
	return c.baseURL
}

// SetToken sets the Bearer token for authenticated requests.
func (c *Client) SetToken(token string) {
	c.token = token
}

// Token returns the current Bearer token.
func (c *Client) Token() string {
	return c.token
}

// Login authenticates with the Ground Control server and returns a session token.
func (c *Client) Login(username, password string) (*LoginResponse, error) {
	reqBody := LoginRequest{
		Username: username,
		Password: password,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal login request: %w", err)
	}

	resp, err := c.doRequest(http.MethodPost, "/login", nil, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("login request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}

	var loginResp LoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		return nil, fmt.Errorf("decode login response: %w", err)
	}

	c.token = loginResp.Token
	return &loginResp, nil
}

// ListSatellites retrieves all satellites.
func (c *Client) ListSatellites() ([]Satellite, error) {
	resp, err := c.doAuthenticatedRequest(http.MethodGet, "/api/satellites", nil)
	if err != nil {
		return nil, fmt.Errorf("list satellites: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}

	var satellites []Satellite
	if err := json.NewDecoder(resp.Body).Decode(&satellites); err != nil {
		return nil, fmt.Errorf("decode satellites response: %w", err)
	}

	return satellites, nil
}

// GetSatellite retrieves a single satellite by name.
func (c *Client) GetSatellite(name string) (*Satellite, error) {
	path := "/api/satellites/" + name
	resp, err := c.doAuthenticatedRequest(http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("get satellite %q: %w", name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}

	var satellite Satellite
	if err := json.NewDecoder(resp.Body).Decode(&satellite); err != nil {
		return nil, fmt.Errorf("decode satellite response: %w", err)
	}

	return &satellite, nil
}

// HealthCheck checks if the Ground Control server is healthy.
func (c *Client) HealthCheck() error {
	resp, err := c.doRequest(http.MethodGet, "/health", nil, nil)
	if err != nil {
		return fmt.Errorf("health check: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	return nil
}

// doRequest performs an HTTP request with optional auth and body.
func (c *Client) doRequest(method, path string, headers map[string]string, body io.Reader) (*http.Response, error) {
	url := c.baseURL + path

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	return c.httpClient.Do(req)
}

// doAuthenticatedRequest performs an HTTP request with the Bearer token.
func (c *Client) doAuthenticatedRequest(method, path string, body io.Reader) (*http.Response, error) {
	headers := map[string]string{}
	if c.token != "" {
		headers["Authorization"] = "Bearer " + c.token
	}
	return c.doRequest(method, path, headers, body)
}

// configFilePath returns the path to the config file for the given server URL.
func configFilePath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory: %w", err)
	}

	dir := filepath.Join(homeDir, configDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create config directory: %w", err)
	}

	return filepath.Join(dir, "config.json"), nil
}

// ConfigData is stored in the config file.
type ConfigData struct {
	Server    string `json:"server"`
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

// SaveConfig saves the client configuration to disk.
func SaveConfig(cfg *ConfigData) error {
	path, err := configFilePath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}

	return nil
}

// LoadConfig loads the client configuration from disk.
func LoadConfig() (*ConfigData, error) {
	path, err := configFilePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ConfigData{}, nil
		}
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var cfg ConfigData
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	return &cfg, nil
}

// parseError reads an error response from the API.
// Handles both formats: {"message":"...","code":N} and {"error":"..."}.
func parseError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)

	var errResp ErrorResponse
	if err := json.Unmarshal(body, &errResp); err == nil {
		if errResp.Message != "" {
			return fmt.Errorf("API error (status %d): %s", resp.StatusCode, errResp.Message)
		}
	}

	var simpleErr struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &simpleErr); err == nil && simpleErr.Error != "" {
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, simpleErr.Error)
	}

	return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
}
