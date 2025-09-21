package sources

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"comicrawl/internal/s3"
)

type Chapter struct {
	Number    string
	Title     string
	URL       string
	Pages     []Page
	SourceURL string
}

type Page struct {
	Number int
	URL    string
}

type Series struct {
	Slug        string
	Title       string
	Description string
	Author      string
	Status      string
	Genres      []string
	Chapters    []Chapter
}

// Source defines the interface that all manga sources must implement
type Source interface {
	// Name returns the unique name of this source
	Name() string
	
	// BaseURL returns the base URL for this source
	BaseURL() string
	
	// ListSeries returns all series available from this source
	ListSeries(ctx context.Context, client *http.Client) ([]Series, error)
	
	// FetchChapters fetches all chapters for a specific series
	FetchChapters(ctx context.Context, client *http.Client, series Series) ([]Chapter, error)
	
	// FetchPages fetches all page URLs for a specific chapter
	FetchPages(ctx context.Context, client *http.Client, chapter Chapter) ([]Page, error)
}

// BaseSource provides common functionality for all sources
type BaseSource struct {
	name    string
	baseURL string
	logger  *slog.Logger
}

func NewBaseSource(name, baseURL string, logger *slog.Logger) *BaseSource {
	return &BaseSource{
		name:    name,
		baseURL: strings.TrimRight(baseURL, "/"),
		logger:  logger,
	}
}

func (b *BaseSource) Name() string {
	return b.name
}

func (b *BaseSource) BaseURL() string {
	return b.baseURL
}

// Helper methods for common source operations

func (b *BaseSource) BuildURL(path string) string {
	return fmt.Sprintf("%s/%s", b.baseURL, strings.TrimLeft(path, "/"))
}

func (b *BaseSource) ExtractSlugFromURL(urlStr string) (string, error) {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	
	// Extract path segments and use the last non-empty segment as slug
	path := strings.Trim(parsed.Path, "/")
	segments := strings.Split(path, "/")
	
	for i := len(segments) - 1; i >= 0; i-- {
		if segments[i] != "" {
			return segments[i], nil
		}
	}
	
	return "", fmt.Errorf("could not extract slug from URL: %s", urlStr)
}

func (b *BaseSource) NormalizeChapterNumber(chapterNum string) string {
	// Remove non-numeric characters and normalize decimal points
	re := regexp.MustCompile(`[^0-9.]`)
	normalized := re.ReplaceAllString(chapterNum, "")
	
	// Ensure it starts with a digit
	if normalized == "" {
		return "0"
	}
	
	// Remove leading zeros from integer parts
	parts := strings.Split(normalized, ".")
	if len(parts) > 0 && parts[0] != "" {
		// Remove leading zeros from integer part
		parts[0] = strings.TrimLeft(parts[0], "0")
		if parts[0] == "" {
			parts[0] = "0"
		}
	}
	
	return strings.Join(parts, ".")
}

func (b *BaseSource) CompareChapters(localChapters []s3.Chapter, remoteChapters []Chapter) (newChapters []Chapter, updatedChapters []Chapter) {
	localMap := make(map[string]s3.Chapter)
	for _, chap := range localChapters {
		localMap[chap.Number] = chap
	}
	
	for _, remoteChap := range remoteChapters {
		localChap, exists := localMap[remoteChap.Number]
		
		if !exists {
			// New chapter
			newChapters = append(newChapters, remoteChap)
		} else if remoteChap.SourceURL != localChap.SourceURL {
			// Chapter exists but source URL changed (indicating update)
			updatedChapters = append(updatedChapters, remoteChap)
		}
		// If chapter exists and source URL is the same, no action needed
	}
	
	return newChapters, updatedChapters
}

// ExampleSource implementation moved to source/example.go