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

	"comicrawl/internal/cloudflare"
	"comicrawl/internal/config"

)

type HTTPClient struct {
	client    *http.Client
	logger    *slog.Logger
	userAgent string
}

func NewHTTPClient(cfg *config.Config, logger *slog.Logger, flareClient *cloudflare.Client) (*HTTPClient, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create cookie jar: %w", err)
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: false,
			MinVersion:         tls.VersionTLS12,
		},
		MaxIdleConns:          1000,
		MaxIdleConnsPerHost:   200,
		MaxConnsPerHost:       0,
		IdleConnTimeout:       30 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		TLSHandshakeTimeout:   30 * time.Second,
		ExpectContinueTimeout: 30 * time.Second,
		DisableKeepAlives:     false,
		DisableCompression:    false,
		WriteBufferSize:       32768,
		ReadBufferSize:        32768,
		ForceAttemptHTTP2:     true,
	}

	if cfg.HTTPProxy != "" {
		proxyURL, err := url.Parse(cfg.HTTPProxy)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL: %w", err)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}

	httpClient := &http.Client{
		Timeout:   180 * time.Second,
		Jar:       jar,
		Transport: transport,
	}

	return &HTTPClient{
		client:    httpClient,
		logger:    logger,
		userAgent: cfg.UserAgent,
	}, nil
}

func (h *HTTPClient) Client() *HTTPClient {
	return h
}

func (h *HTTPClient) Do(req *http.Request) (*http.Response, error) {
	if h.userAgent != "" {
		req.Header.Set("User-Agent", h.userAgent)
	}

	h.logger.Debug("HTTP request",
		"method", req.Method,
		"url", req.URL.String(),
		"headers", req.Header)

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

func (h *HTTPClient) ConfigureForDomain(ctx context.Context, domain string, flareClient *cloudflare.Client, proxyURL string) error {
	if flareClient == nil {
		return nil // No Cloudflare bypass configured
	}

	solution, err := flareClient.CreateSession(ctx, domain, proxyURL)
	if err != nil {
		return fmt.Errorf("failed to get Cloudflare session for %s: %w", domain, err)
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

	// Update user agent if provided by Cloudflare client
	if solution.UserAgent != "" {
		h.userAgent = solution.UserAgent
	}

	h.logger.Info("configured HTTP client for domain",
		"domain", domain,
		"cookies", len(solution.Cookies),
		"userAgent", solution.UserAgent,
		"proxy", proxyURL)

	return nil
}
