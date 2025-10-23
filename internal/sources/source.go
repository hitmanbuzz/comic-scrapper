package sources

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"

	"comicrawl/internal/disk"
	"comicrawl/internal/httpclient"
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

type Source interface {
	GetName() string
	GetBaseURL() string
	ListSeries(ctx context.Context, client *httpclient.HTTPClient) ([]Series, error)
	FetchChapters(ctx context.Context, client *httpclient.HTTPClient, series Series) ([]Chapter, error)
	FetchPages(ctx context.Context, client *httpclient.HTTPClient, chapter Chapter) ([]Page, error)
}

type BaseSource struct {
	Name    string
	BaseURL string
	Logger  *slog.Logger
}

func NewBaseSource(name, baseURL string, logger *slog.Logger) *BaseSource {
	return &BaseSource{
		Name:    name,
		BaseURL: strings.TrimRight(baseURL, "/"),
		Logger:  logger,
	}
}

func (b *BaseSource) GetName() string {
	return b.Name
}

func (b *BaseSource) GetBaseURL() string {
	return b.BaseURL
}

func (b *BaseSource) BuildURL(path string) string {
	return fmt.Sprintf("%s/%s", b.BaseURL, strings.TrimLeft(path, "/"))
}

func (b *BaseSource) ExtractSlugFromURL(urlStr string) (string, error) {
	if urlStr == "" {
		return "", fmt.Errorf("empty URL")
	}
	
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}

	path := strings.Trim(parsed.Path, "/")
	if path == "" {
		return "", fmt.Errorf("empty path in URL: %s", urlStr)
	}
	
	segments := strings.Split(path, "/")

	// General case: take the last non-empty segment
	for i := len(segments) - 1; i >= 0; i-- {
		if segments[i] != "" {
			return segments[i], nil
		}
	}

	return "", fmt.Errorf("could not extract slug from URL: %s", urlStr)
}

func (b *BaseSource) NormalizeChapterNumber(chapterNum string) string {
	// First extract the number using a more sophisticated regex
	re := regexp.MustCompile(`(\d+(?:\.\d+)?)`)
	matches := re.FindStringSubmatch(chapterNum)
	
	if len(matches) == 0 {
		return "0"
	}
	
	normalized := matches[1]
	
	parts := strings.Split(normalized, ".")
	if len(parts) > 0 && parts[0] != "" {
		parts[0] = strings.TrimLeft(parts[0], "0")
		if parts[0] == "" {
			parts[0] = "0"
		}
	}

	return strings.Join(parts, ".")
}

func (b *BaseSource) CompareChapters(localChapters []disk.Chapter, remoteChapters []Chapter) (newChapters []Chapter, updatedChapters []Chapter) {
    localMap := make(map[string]disk.Chapter)
    for _, chap := range localChapters {
        localMap[chap.Number] = chap
    }
    
    for _, remoteChap := range remoteChapters {
        localChap, exists := localMap[remoteChap.Number]
        if !exists {
            newChapters = append(newChapters, remoteChap)
        } else if remoteChap.SourceURL != localChap.SourceURL {
            updatedChapters = append(updatedChapters, remoteChap)
        }
    }
    return newChapters, updatedChapters
}
