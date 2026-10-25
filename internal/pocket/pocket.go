// Package pocket talks to sibling Pocket apps: Pocket Inventory (the item a
// device is: purchase date, warranty, where it lives) and Pocket Monitoring
// (the health of a server or NAS). Same bearer-token JSON API everywhere.
package pocket

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
	"time"
)

// Client is a bearer-token JSON client for one Pocket app.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// New creates a client.
func New(baseURL, token string, timeout time.Duration) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTP: &http.Client{Timeout: timeout}}
}

// Configured reports whether URL and token are set.
func (c *Client) Configured() bool { return c != nil && c.BaseURL != "" && c.Token != "" }

// ErrNotFound is a 404 from the sibling app.
var ErrNotFound = errors.New("not found")

func (c *Client) do(ctx context.Context, method, path string, q url.Values, body any, v any) error {
	if !c.Configured() {
		return errors.New("not configured")
	}
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized {
		return errors.New("token rejected (401)")
	}
	if res.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if res.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		b, _ := io.ReadAll(io.LimitReader(res.Body, 2000))
		_ = json.Unmarshal(b, &e)
		if e.Error != "" {
			return fmt.Errorf("HTTP %d: %s", res.StatusCode, e.Error)
		}
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	if v == nil || res.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(v)
}

// Health is the connection test for any Pocket app: /healthz plus a token check.
func (c *Client) Health(ctx context.Context) (string, error) {
	var out struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if !c.Configured() {
		return "", errors.New("not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/healthz", nil)
	if err != nil {
		return "", err
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", res.StatusCode)
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	var me map[string]any
	if err := c.do(ctx, http.MethodGet, "/api/v1/auth/me", nil, nil, &me); err != nil {
		return out.Version, err
	}
	return out.Version, nil
}

// ---------------- Pocket Inventory ----------------

// Item is a Pocket Inventory item (the fields a device page shows).
type Item struct {
	ID           int64   `json:"id"`
	Name         string  `json:"name"`
	Brand        string  `json:"brand"`
	Model        string  `json:"model"`
	Serial       string  `json:"serial"`
	PurchaseDate string  `json:"purchase_date"`
	Price        float64 `json:"price"`
	ExpiryDate   string  `json:"expiry_date"`
	LocationName string  `json:"location_name"`
	CategoryName string  `json:"category_name"`
	Notes        string  `json:"notes"`
	URL          string  `json:"url"`
	Archived     bool    `json:"archived"`
}

// Items searches the person's items.
func (c *Client) Items(ctx context.Context, q string, limit int) ([]Item, error) {
	v := url.Values{}
	if q != "" {
		v.Set("q", q)
	}
	if limit > 0 {
		v.Set("limit", fmt.Sprint(limit))
	}
	var out []Item
	if err := c.do(ctx, http.MethodGet, "/api/v1/items", v, nil, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []Item{}
	}
	return out, nil
}

// Item loads one item.
func (c *Client) Item(ctx context.Context, id int64) (*Item, error) {
	var out struct {
		Item Item `json:"item"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/items/%d", id), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out.Item, nil
}

// ---------------- Pocket Monitoring ----------------

// Server is a Pocket Monitoring server with its headline numbers.
type Server struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	LastSeen  string `json:"last_seen"`
	LastError string `json:"last_error"`
	Summary   *struct {
		Hostname string  `json:"hostname"`
		Platform string  `json:"platform"`
		CPU      float64 `json:"cpu"`
		MemPct   float64 `json:"mem_pct"`
		Temp     float64 `json:"temp"`
		FSPct    float64 `json:"fs_pct"`
		Uptime   float64 `json:"uptime_s"`
	} `json:"summary"`
	AlertsActive int `json:"alerts_active"`
}

// Servers lists the person's servers.
func (c *Client) Servers(ctx context.Context) ([]Server, error) {
	var out []Server
	if err := c.do(ctx, http.MethodGet, "/api/v1/servers", nil, nil, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []Server{}
	}
	return out, nil
}

// Server loads one server.
func (c *Client) Server(ctx context.Context, id int64) (*Server, error) {
	var out Server
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/servers/%d", id), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
