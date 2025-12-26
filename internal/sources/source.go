package sources

import (
	"context"
	"log/slog"
	"strings"

	"comicrawl/internal/cstructs/scrape_data"
	"comicrawl/internal/httpclient"
)

type Chapter struct {
	// Chapter Number
	Number    float32
	// Chapter Title (optional)
	Title     string
	// Chapter URL
	URL       string
}

// Page = Image
type Page struct {
	// Page Number
	Number int
	// Page/Image URL
	URL    string
}

type Series struct {
	// URL to the comic page on scanlator website (a.k.a comic page)
	URL string
	// Title fetch from the scanlator website for the comic
	Title string
	// Current Comic Status from scanlator website
	Status string
}

type Source interface {
	// Get the Source Provider Name (aka Scanlator name)
	GetName() string
	// Get the base URL for the source provider site
	GetBaseURL() string
	// Get the group (source provider) IDs on MU (can have multiple)
	GetMuGroupIDs() []int64
	// It will not be use directly in scanlator code but only for generating those series data in a json file
	ListSeries(ctx context.Context, client *httpclient.HTTPClient) (scrape_data.FullSeriesResponse, error)
	FetchChapters(ctx context.Context, client *httpclient.HTTPClient, series Series) ([]Chapter, error)
	FetchPages(ctx context.Context, client *httpclient.HTTPClient, chapter Chapter) ([]Page, error)
}

type BaseSource struct {
	// Scanlator Name
	Name       string
	// Scanlator Base URL
	BaseURL    string
	// Scanlators IDs from MU
	MuGroupIDs []int64
	Logger     *slog.Logger
}

func NewBaseSource(name, baseURL string, muGroupIds []int64, logger *slog.Logger) *BaseSource {
	return &BaseSource{
		Name:       name,
		BaseURL:    strings.TrimRight(baseURL, "/"),
		MuGroupIDs: muGroupIds,
		Logger:     logger,
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

// Get the group (source provider) IDs on MU (can have multiple)
func (b *BaseSource) GetMuGroupIDs() []int64 {
	return b.MuGroupIDs
}
