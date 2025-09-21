package httpclient

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"

	"golang.org/x/time/rate"
	"comicrawl/internal/config"
	"comicrawl/internal/flaresolverr"
)

type HTTPClient struct {
	client      *http.Client
	limiter     *rate.Limiter
	logger      *slog.Logger
	userAgent   string
}

func NewHTTPClient(cfg *config.Config, logger *slog.Logger, flareClient *flaresolverr.Client) (*HTTPClient, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create cookie jar: %w", err)
	}

	httpClient := &http.Client{
		Timeout: cfg.RequestTimeout,
		Jar:     jar,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: false,
			},
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	limiter := rate.NewLimiter(rate.Limit(cfg.RequestsPerSecond), 1)

	return &HTTPClient{
		client:    httpClient,
		limiter:   limiter,
		logger:    logger,
		userAgent: cfg.UserAgent,
	}, nil
}

// Client returns the underlying http.Client for direct use
func (h *HTTPClient) Client() *http.Client {
	return h.client
}

func (h *HTTPClient) Do(req *http.Request) (*http.Response, error) {
	// Apply rate limiting
	if err := h.limiter.Wait(req.Context()); err != nil {
		return nil, fmt.Errorf("rate limiter error: %w", err)
	}

	// Set user agent
	if h.userAgent != "" {
		req.Header.Set("User-Agent", h.userAgent)
	}

	h.logger.Debug("HTTP request", 
		"method", req.Method, 
		"url", req.URL.String(),
		"headers", req.Header)

	// Execute request with retries
	var resp *http.Response
	var err error
	
	for attempt := 1; attempt <= 3; attempt++ {
		resp, err = h.client.Do(req)
		if err == nil && resp.StatusCode < 500 {
			break
		}
		
		if err != nil {
			h.logger.Warn("HTTP request failed", 
				"attempt", attempt, 
				"error", err,
				"url", req.URL.String())
		} else {
			resp.Body.Close()
			h.logger.Warn("HTTP request failed with status", 
				"attempt", attempt, 
				"status", resp.StatusCode,
				"url", req.URL.String())
		}
		
		if attempt < 3 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed after 3 attempts: %w", err)
	}

	if resp.StatusCode >= 400 {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP error: %s", resp.Status)
	}

	h.logger.Debug("HTTP response", 
		"status", resp.StatusCode,
		"url", req.URL.String())

	return resp, nil
}

func (h *HTTPClient) ConfigureForDomain(ctx context.Context, domain string, flareClient *flaresolverr.Client) error {
	if flareClient == nil {
		return nil // No FlareSolverr configured
	}

	solution, err := flareClient.GetSession(ctx, domain)
	if err != nil {
		return fmt.Errorf("failed to get FlareSolverr session for %s: %w", domain, err)
	}

	// Update cookies for the domain
	jar := h.client.Jar
	if jar != nil {
		url, err := url.Parse(domain)
		if err != nil {
			return fmt.Errorf("invalid domain: %w", err)
		}
		jar.SetCookies(url, solution.CreateCookieJar())
	}

	// Update user agent if provided by FlareSolverr
	if solution.UserAgent != "" {
		h.userAgent = solution.UserAgent
	}

	h.logger.Info("configured HTTP client for domain", 
		"domain", domain, 
		"cookies", len(solution.Cookies),
		"userAgent", solution.UserAgent)

	return nil
}