// Package cloudflare provides a client for FlareSolver-compatible services
// that can provide Cloudflare clearance cookies/challenge solutions.
package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"comicrawl/internal/config"
)

type Client struct {
	baseURL string
	client  *http.Client
	logger  *slog.Logger
}

type SessionResponse struct {
	Status    string   `json:"status"`
	Message   string   `json:"message"`
	Solution  Solution `json:"solution"`
	StartTime int64    `json:"startTimestamp"`
	EndTime   int64    `json:"endTimestamp"`
	Version   string   `json:"version"`
}

type Solution struct {
	URL       string            `json:"url"`
	Status    int               `json:"status"`
	Cookies   []Cookie          `json:"cookies"`
	UserAgent string            `json:"userAgent"`
	Headers   map[string]string `json:"headers"`
}

type Cookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires"`
	Secure   bool    `json:"secure"`
	HTTPOnly bool    `json:"httpOnly"`
}

type Request struct {
	Cmd        string            `json:"cmd"`
	URL        string            `json:"url"`
	UserAgent  string            `json:"userAgent,omitempty"`
	MaxTimeout int               `json:"maxTimeout,omitempty"`
	Proxy      map[string]string `json:"proxy,omitempty"`
}

// NewClient creates a new Cloudflare bypass client for FlareSolver-compatible services.
func NewClient(cfg *config.Config, logger *slog.Logger) *Client {
	return &Client{
		baseURL: cfg.CloudflareURL,
		client: &http.Client{},
		logger: logger,
	}
}

// GetSession requests a Cloudflare challenge solution from a FlareSolver-compatible service.
// It returns the solution containing cookies and headers needed to bypass Cloudflare protection.
func (c *Client) GetSession(ctx context.Context, targetURL string, proxyURL string) (*Solution, error) {
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("invalid target URL: %w", err)
	}

	domain := fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Host)

	request := Request{
		Cmd:        "request.get",
		URL:        domain,
		MaxTimeout: 60000, // 60 seconds
	}

	if proxyURL != "" {
		request.Proxy = map[string]string{
			"url": proxyURL,
		}
	}

	c.logger.Debug("requesting FlareSolver-compatible session", "url", domain, "proxy", proxyURL)

	resp, err := c.doRequest(ctx, request)
	if err != nil {
		return nil, err
	}

	if resp.Status != "ok" {
		return nil, fmt.Errorf("flare solver compatible client error: %s", resp.Message)
	}

	c.logger.Info("obtained FlareSolver-compatible session",
		"url", domain,
		"cookies", len(resp.Solution.Cookies),
		"userAgent", resp.Solution.UserAgent,
		"proxy", proxyURL)

	return &resp.Solution, nil
}

// doRequest sends a request to the FlareSolver-compatible service and parses the response.
func (c *Client) doRequest(ctx context.Context, req Request) (*SessionResponse, error) {
	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var sessionResp SessionResponse
	if err := json.Unmarshal(body, &sessionResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &sessionResp, nil
}

// CreateCookieJar converts the solution cookies to HTTP cookies that can be used in requests.
func (s *Solution) CreateCookieJar() []*http.Cookie {
	var cookies []*http.Cookie
	for _, cookie := range s.Cookies {
		var expires time.Time
		if cookie.Expires > 0 {
			expires = time.Unix(int64(cookie.Expires), 0)
		}

		cookies = append(cookies, &http.Cookie{
			Name:     cookie.Name,
			Value:    cookie.Value,
			Domain:   cookie.Domain,
			Path:     cookie.Path,
			Expires:  expires,
			Secure:   cookie.Secure,
			HttpOnly: cookie.HTTPOnly,
		})
	}
	return cookies
}
