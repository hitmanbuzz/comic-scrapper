package sources

import (
	"context"
	"log/slog"
	"strings"

	"comicrawl/internal/cstructs"
	"comicrawl/internal/httpclient"
)

type Chapter struct {
	// Chapter Number
	Number    float32
	// Chapter Title (optional)
	Title     string
	// Chapter URL
	URL       string
	// Contain the images page data
	Pages     []Page
}

type Page struct {
	Number int
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
	ListSeries(ctx context.Context, client *httpclient.HTTPClient) (cstructs.FullSeriesResponse, error)
	FetchChapters(ctx context.Context, client *httpclient.HTTPClient, series Series) ([]Chapter, error)
	FetchPages(ctx context.Context, client *httpclient.HTTPClient, chapter Chapter) ([]Page, error)
}

type BaseSource struct {
	Name       string
	BaseURL    string
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
