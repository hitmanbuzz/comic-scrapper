package scanlators

import (
	"comicrawl/internal/cstructs"
	"comicrawl/internal/httpclient"
	"comicrawl/internal/sources"
	"comicrawl/internal/util"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type HiveScans struct {
	*sources.BaseSource
}

func NewHiveScans(logger *slog.Logger) *HiveScans {
	return &HiveScans{
		BaseSource: sources.NewBaseSource("hivescans", "https://hivetoons.org", util.ParseSlugsToIds(util.HiveScans), logger),
	}
}

// API Response Structures
type HiveSeriesMetadata struct {
	Posts []HiveSeriesItem `json:"posts"`
}

type HiveSeriesItem struct {
	ID        int    `json:"id"`
	Slug      string `json:"slug"`
	PostTitle string `json:"postTitle"`
	Status    string `json:"seriesStatus"`
}

type HiveChaptersResponse struct {
	Post struct {
		Chapters []HiveChapter `json:"chapters"`
	} `json:"post"`
}

type HiveChapter struct {
	Slug         string  `json:"slug"`
	Number       float64 `json:"number"`
	Title        string  `json:"title"`
	IsLocked     bool    `json:"isLocked"`
	IsAccessible bool    `json:"isAccessible"`
}

func (h *HiveScans) ListSeries(ctx context.Context, client *httpclient.HTTPClient) (cstructs.FullSeriesResponse, error) {
	h.Logger.Info("fetching series list from HiveScans")

	var allSeries cstructs.FullSeriesResponse

	url := "https://api.hivetoons.org/api/query?page=1&perPage=100000"

	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Origin", h.GetBaseURL())
	req.Header.Set("Referer", h.GetBaseURL()+"/")

	resp, err := client.Do(req)
	if err != nil {
		return allSeries, fmt.Errorf("failed to fetch series metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return allSeries, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var metadata HiveSeriesMetadata
	if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
		return allSeries, fmt.Errorf("failed to decode JSON: %w", err)
	}

	allSeries.GroupName = h.GetName()
	allSeries.MuGroupIds = util.ParseSlugsToIds(util.HiveScans)
	allSeries.TotalSeries = len(metadata.Posts)

	for _, item := range metadata.Posts {
		// Store series ID in URL path for later extraction
		seriesURL := fmt.Sprintf("%s/series/%d/%s", h.GetBaseURL(), item.ID, item.Slug)
		allSeries.Series = append(allSeries.Series, cstructs.ScanSeriesResponse{
			MainTitle:    item.PostTitle,
			ComicPageUrl: seriesURL,
			MuSeriesId:   -1,
			ComicStatus:  item.Status,
			Found:        false,
		})
	}

	h.Logger.Info("fetched series from HiveScans", "count", len(allSeries.Series))
	return allSeries, nil
}

func (h *HiveScans) FetchChapters(ctx context.Context, client *httpclient.HTTPClient, series sources.Series) ([]sources.Chapter, error) {
	// Extract series ID and slug from URL
	seriesID, seriesSlug, err := h.parseSeriesURL(series.URL)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("https://api.hivetoons.org/api/chapters?postId=%d", seriesID)

	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Origin", h.GetBaseURL())
	req.Header.Set("Referer", h.GetBaseURL()+"/")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch chapters: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var chaptersResp HiveChaptersResponse
	if err := json.NewDecoder(resp.Body).Decode(&chaptersResp); err != nil {
		return nil, fmt.Errorf("failed to decode JSON: %w", err)
	}

	return h.parseChapters(chaptersResp.Post.Chapters, seriesSlug), nil
}

func (h *HiveScans) parseChapters(hiveChapters []HiveChapter, seriesSlug string) []sources.Chapter {
	var chapters []sources.Chapter

	for _, ch := range hiveChapters {
		if !ch.IsAccessible && ch.IsLocked {
			continue
		}

		chapterNum := fmt.Sprintf("%.0f", ch.Number)
		chapterURL := fmt.Sprintf("%s/series/%s/%s", h.GetBaseURL(), seriesSlug, ch.Slug)
		chapters = append(chapters, sources.Chapter{
			Number:    h.NormalizeChapterNumber(chapterNum),
			Title:     ch.Title,
			URL:       chapterURL,
			SourceURL: chapterURL,
		})
	}

	return chapters
}

func (h *HiveScans) FetchPages(ctx context.Context, client *httpclient.HTTPClient, chapter sources.Chapter) ([]sources.Page, error) {
	h.Logger.Info("fetching pages", "chapter", chapter.Number)

	req, err := http.NewRequestWithContext(ctx, "GET", chapter.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pages: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	return h.parsePages(doc)
}

func (h *HiveScans) parsePages(doc *goquery.Document) ([]sources.Page, error) {
	var pages []sources.Page

	// Extract all image URLs directly from HTML
	doc.Find("img").Each(func(i int, s *goquery.Selection) {
		imageURL := s.AttrOr("src", "")
		if imageURL == "" {
			imageURL = s.AttrOr("data-src", "")
		}
		if imageURL == "" {
			imageURL = s.AttrOr("data-lazy-src", "")
		}

		// Only include images from storage.hivetoon.com with /upload/series/ path
		if strings.Contains(imageURL, "storage.hivetoon.com") && strings.Contains(imageURL, "/upload/series/") {
			pages = append(pages, sources.Page{
				Number: len(pages),
				URL:    imageURL,
			})
		}
	})

	h.Logger.Info("parsed pages", "count", len(pages))
	return pages, nil
}

// parseSeriesURL extracts series ID and slug from the series URL
func (h *HiveScans) parseSeriesURL(seriesURL string) (int, string, error) {
	// Extract ID and slug from URL like https://hivetoons.org/series/123/the-warrior-king
	re := regexp.MustCompile(`/series/(\d+)/([^/]+)`)
	matches := re.FindStringSubmatch(seriesURL)
	if len(matches) < 3 {
		return 0, "", fmt.Errorf("invalid series URL format: %s", seriesURL)
	}
	seriesID, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, "", fmt.Errorf("invalid series ID in URL: %s", seriesURL)
	}
	return seriesID, matches[2], nil
}
