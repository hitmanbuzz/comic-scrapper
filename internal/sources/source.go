package sources

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"comicrawl/internal/cstructs"
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
	// URL to the comic page on scanlator website (a.k.a comic page)
	URL         string
	// Title fetch from the scanlator website for the comic
	Title       string
	// Current Comic Status from scanlator website
	Status      string
}

type Source interface {
	// Get the Source Provider Name (aka Scanlator name)
	GetName() string
	// Get the base URL for the source provider site
	GetBaseURL() string
	// Get the group (source provider) ID on MU
	GetMuGroupID() int64
	// It will not be use directly in scanlator code but only for generating those series data in a json file
	ListSeries(ctx context.Context, client *httpclient.HTTPClient) (cstructs.FullSeriesResponse, error)
	FetchChapters(ctx context.Context, client *httpclient.HTTPClient, series Series) ([]Chapter, error)
	FetchPages(ctx context.Context, client *httpclient.HTTPClient, chapter Chapter) ([]Page, error)
}

type BaseSource struct {
	Name         string
	BaseURL      string
	MuGroupID    int64
	Logger       *slog.Logger
}

func NewBaseSource(name, baseURL string, muGroupId int64, logger *slog.Logger) *BaseSource {
	return &BaseSource{
		Name:    name,
		BaseURL: strings.TrimRight(baseURL, "/"),
		MuGroupID: muGroupId,
		Logger:  logger,
	}
}

// Get the Source Provider Name (aka Scanlator name)
func (b *BaseSource) GetName() string {
	return b.Name
}

// Get the base URL for the source provider site
func (b *BaseSource) GetBaseURL() string {
	return b.BaseURL
}

// Get the group (source provider) ID on MU
func (b *BaseSource) GetMuGroupID() int64 {
	return b.MuGroupID
}

// NOTE: NOT NEEDED (will think about it)
func (b *BaseSource) BuildURL(path string) string {
	return fmt.Sprintf("%s/%s", b.BaseURL, strings.TrimLeft(path, "/"))
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

// NOTE: NOT NEEDED (can be update so that it fits with our new codebase)
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
