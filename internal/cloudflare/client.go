package cloudflare

import (
	"bytes"
	"comicrawl/internal/config"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	flareURL string
	client  *http.Client
	logger  *slog.Logger
}

// Main Flaresolverr Response Here
type FlareResponse struct {
	Solution   SolutionResp  `json:"solution"`
	Status     string        `json:"status"`
	Message   string         `json:"message"`
}

type SolutionResp struct {
	URL         string                 `json:"url"`	
	Status      int                    `json:"status"`
	Headers     map[string]string      `json:"headers"`
	Cookies     []Cookie               `json:"cookies"`
	UserAgent   string                 `json:"userAgent"`
}

type Cookie struct {
	Name     string    `json:"name"`
	Value    string    `json:"value"`
	Domain   string    `json:"domain"`
	Expires  float64   `json:"expires"`
	HttpOnly bool      `json:"httpOnly"`
	Secure   bool      `json:"secure"`
}
// --- End Here ---

// The Payload request to be send to flaresolverr
type FlareRequest struct {
	Cmd         string   `json:"cmd"`
	URL         string   `json:"url"`
	UserAgent   string   `json:"userAgent,omitempty"`
	MaxTimeout  int      `json:"maxTimeout,omitempty"`
	Proxy       map[string]string `json:"proxy,omitempty"`
}

// Create a new flarsolverr client instance
func NewFlareClient(cfg *config.Config, logger *slog.Logger) *Client {
	return &Client{
		flareURL: cfg.FlareSolverrURL,
		client: &http.Client{},
		logger: logger,
	}
}

// Create a session where it passed the flaresolverr cookies from 3rd arg to the GO http.Client implementation
//
// Modified the http.Client implementation (the 1st arg)
func (c *Client) CreateSession(ctx context.Context, targetURL string, proxyURL string) (*SolutionResp, error) {
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("Failed to parse url: %s\n", targetURL)
	}
	
	// Get the baseURL of the targetURL
	domain := fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Host)
	
	payload := FlareRequest {
		Cmd: "request.get",
		URL: domain,
		MaxTimeout: 60000, // 60s or 60000ms
	}

	// Insert proxt if found
	if proxyURL != "" {
		payload.Proxy = map[string]string {
			"url": proxyURL,
		}
	}	

	resp, err := c.doRequest(ctx, payload)
	if err != nil {
		return nil, err
	}

	if resp.Status != "ok" {
		return nil, fmt.Errorf("Flaresolverr client errir: %s", resp.Message)
	}

	c.logger.Info(
		"flaresolverr session success",
		"url", domain,
		"cookies count", len(resp.Solution.Cookies),
		"user-agent", resp.Solution.UserAgent,
		"proxy", proxyURL, 
	)

	return &resp.Solution, nil
}

func (c *Client) doRequest(ctx context.Context, payload FlareRequest) (*FlareResponse, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.flareURL, bytes.NewReader(jsonData))
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

	var flareResp FlareResponse
	if err := json.Unmarshal(body, &flareResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &flareResp, nil
}

func (fs *SolutionResp) CreateCookieJar() []*http.Cookie {
	var cookies []*http.Cookie
	for _, cookie := range fs.Cookies {
		var expires time.Time
		if cookie.Expires > 0 {
			expires = time.Unix(int64(cookie.Expires), 0)
		}

		cookies = append(cookies, &http.Cookie{
			Name:     cookie.Name,
			Value:    cookie.Value,
			Domain:   cookie.Domain,
			Expires:  expires,
			Secure:   cookie.Secure,
			HttpOnly: cookie.HttpOnly,
		})
	}
	return cookies
}
