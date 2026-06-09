package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client is a typed HTTP client for the Ground Control API.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// New creates a new Ground Control client.
func New(baseURL, token string) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Satellite represents a satellite registered in Ground Control.
type Satellite struct {
	ID                int32      `json:"id"`
	Name              string     `json:"name"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	LastSeen          *time.Time `json:"last_seen,omitempty"`
	HeartbeatInterval *string    `json:"heartbeat_interval,omitempty"`
}

// SatelliteStatus represents the latest sync status of a satellite.
type SatelliteStatus struct {
	Activity           string    `json:"activity"`
	LatestStateDigest  string    `json:"latest_state_digest"`
	LatestConfigDigest string    `json:"latest_config_digest"`
	CPUPercent         string    `json:"cpu_percent"`
	MemoryUsedBytes    int64     `json:"memory_used_bytes"`
	StorageUsedBytes   int64     `json:"storage_used_bytes"`
	LastSyncDurationMs int64     `json:"last_sync_duration_ms"`
	ImageCount         int32     `json:"image_count"`
	ReportedAt         time.Time `json:"reported_at"`
}

// CachedImage represents an image cached by a satellite.
type CachedImage struct {
	Reference string `json:"reference"`
	SizeBytes int64  `json:"size_bytes"`
}

// Group represents a group of images.
type Group struct {
	ID        int32     `json:"id"`
	GroupName string    `json:"group_name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Config represents a satellite configuration.
type Config struct {
	ID         int32     `json:"id"`
	Name       string    `json:"name"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ListSatellitesParams controls filtering and pagination for satellite listing.
type ListSatellitesParams struct {
	Limit      int
	Offset     int
	NamePrefix string
	Sort       string
	Order      string
}

// RegisterSatelliteParams holds parameters for registering a new satellite.
type RegisterSatelliteParams struct {
	Name       string    `json:"name"`
	ConfigName string    `json:"config_name"`
	Groups     *[]string `json:"groups,omitempty"`
}

// RegisterSatelliteResponse is returned by the register endpoint.
type RegisterSatelliteResponse struct {
	Token string `json:"token"`
}

// SatelliteGroupParams holds parameters for add/remove satellite from group.
type SatelliteGroupParams struct {
	Satellite string `json:"satellite"`
	Group     string `json:"group"`
}

// do executes an authenticated HTTP request and decodes the JSON response.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(respBody))
	}

	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}

	return nil
}

// Ping checks if Ground Control is reachable.
func (c *Client) Ping(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/ping", nil, nil)
}

// Health returns the health status of Ground Control.
func (c *Client) Health(ctx context.Context) (map[string]any, error) {
	var result map[string]any
	if err := c.do(ctx, http.MethodGet, "/health", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// ListSatellites returns a list of satellites with optional filtering.
func (c *Client) ListSatellites(ctx context.Context, params ListSatellitesParams) ([]Satellite, error) {
	q := url.Values{}
	if params.Limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", params.Limit))
	}
	if params.Offset > 0 {
		q.Set("offset", fmt.Sprintf("%d", params.Offset))
	}
	if params.NamePrefix != "" {
		q.Set("name_prefix", params.NamePrefix)
	}
	if params.Sort != "" {
		q.Set("sort", params.Sort)
	}
	if params.Order != "" {
		q.Set("order", params.Order)
	}

	path := "/api/satellites"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}

	var result []Satellite
	if err := c.do(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetSatellite returns a single satellite by name.
func (c *Client) GetSatellite(ctx context.Context, name string) (*Satellite, error) {
	var result Satellite
	if err := c.do(ctx, http.MethodGet, "/api/satellites/"+name, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RegisterSatellite registers a new satellite and returns its one-time token.
func (c *Client) RegisterSatellite(ctx context.Context, params RegisterSatelliteParams) (*RegisterSatelliteResponse, error) {
	var result RegisterSatelliteResponse
	if err := c.do(ctx, http.MethodPost, "/api/satellites", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteSatellite removes a satellite by name.
func (c *Client) DeleteSatellite(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, "/api/satellites/"+name, nil, nil)
}

// GetSatelliteStatus returns the latest sync status of a satellite.
func (c *Client) GetSatelliteStatus(ctx context.Context, name string) (*SatelliteStatus, error) {
	var result SatelliteStatus
	if err := c.do(ctx, http.MethodGet, "/api/satellites/"+name+"/status", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetSatelliteImages returns the cached images of a satellite.
func (c *Client) GetSatelliteImages(ctx context.Context, name string) ([]CachedImage, error) {
	var result []CachedImage
	if err := c.do(ctx, http.MethodGet, "/api/satellites/"+name+"/images", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetActiveSatellites returns satellites that have checked in recently.
func (c *Client) GetActiveSatellites(ctx context.Context) ([]Satellite, error) {
	var result []Satellite
	if err := c.do(ctx, http.MethodGet, "/api/satellites/active", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetStaleSatellites returns satellites that have not checked in recently.
func (c *Client) GetStaleSatellites(ctx context.Context) ([]Satellite, error) {
	var result []Satellite
	if err := c.do(ctx, http.MethodGet, "/api/satellites/stale", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// ListGroups returns all image groups.
func (c *Client) ListGroups(ctx context.Context) ([]Group, error) {
	var result []Group
	if err := c.do(ctx, http.MethodGet, "/api/groups", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetGroup returns a single group by name.
func (c *Client) GetGroup(ctx context.Context, name string) (*Group, error) {
	var result Group
	if err := c.do(ctx, http.MethodGet, "/api/groups/"+name, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// AddSatelliteToGroup adds a satellite to a group.
func (c *Client) AddSatelliteToGroup(ctx context.Context, satellite, group string) error {
	return c.do(ctx, http.MethodPost, "/api/groups/satellite", SatelliteGroupParams{
		Satellite: satellite,
		Group:     group,
	}, nil)
}

// RemoveSatelliteFromGroup removes a satellite from a group.
func (c *Client) RemoveSatelliteFromGroup(ctx context.Context, satellite, group string) error {
	return c.do(ctx, http.MethodDelete, "/api/groups/satellite", SatelliteGroupParams{
		Satellite: satellite,
		Group:     group,
	}, nil)
}

// ListConfigs returns all satellite configs.
func (c *Client) ListConfigs(ctx context.Context) ([]Config, error) {
	var result []Config
	if err := c.do(ctx, http.MethodGet, "/api/configs", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetConfig returns a single config by name.
func (c *Client) GetConfig(ctx context.Context, name string) (*Config, error) {
	var result Config
	if err := c.do(ctx, http.MethodGet, "/api/configs/"+name, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteConfig removes a config by name.
func (c *Client) DeleteConfig(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, "/api/configs/"+name, nil, nil)
}
