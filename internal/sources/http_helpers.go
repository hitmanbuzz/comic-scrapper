package sources

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"comicrawl/internal/httpclient"
)

// NOTE: This codebase shouldn't remove unless discuss because it depends on the scanlator source code to which one to use

// HTTPError represents an error from an HTTP request with status code context.
type HTTPError struct {
	StatusCode int
	Message    string
	URL        string
}

func (e *HTTPError) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("HTTP %d: %s (URL: %s)", e.StatusCode, e.Message, e.URL)
	}
	return fmt.Sprintf("%s (URL: %s)", e.Message, e.URL)
}

// FetchWithContext performs an HTTP GET request with standardized logging and error handling.
// It logs the operation, creates the request with context, executes it, and checks the status code.
func FetchWithContext(ctx context.Context, client *httpclient.HTTPClient, logger *slog.Logger, url string, operation string) (*http.Response, error) {
	if logger != nil {
		logger.Debug(operation, "url", url)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, &HTTPError{
			StatusCode: resp.StatusCode,
			Message:    "unexpected status code",
			URL:        url,
		}
	}

	return resp, nil
}
