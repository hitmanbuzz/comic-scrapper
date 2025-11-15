package scanlators

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"comicrawl/internal/httpclient"
	"comicrawl/internal/sources"
)

type FlameComics struct {
	*sources.BaseSource
	dataToken string
}

func NewFlameComics(logger *slog.Logger) *FlameComics {
	return &FlameComics{
		BaseSource: sources.NewBaseSource("flamecomics", "https://flamecomics.xyz", logger),
	}
}

// API Response Structures
type FlameComicsSeriesListItem struct {
	ID     int    `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"`
}

type FlameComicsChaptersResponse struct {
	PageProps struct {
		Chapters []FlameComicsChapter `json:"chapters"`
	} `json:"pageProps"`
}

type FlameComicsChapter struct {
	ChapterID int    `json:"chapter_id"`
	Chapter   string `json:"chapter"`
	Title     string `json:"title"`
	Token     string `json:"token"`
}

type FlameImageInfo struct {
	Name string `json:"name"`
}

type FlameComicsChapterResponse struct {
	PageProps struct {
		Chapter struct {
			Images map[string]FlameImageInfo `json:"images"`
		} `json:"chapter"`
	} `json:"pageProps"`
}

func (f *FlameComics) fetchBuildID(ctx context.Context) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", f.GetBaseURL()+"/", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch homepage: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	re := regexp.MustCompile(`"buildId":"([^"]+)"`)
	if matches := re.FindSubmatch(body); len(matches) >= 2 {
		f.dataToken = string(matches[1])
		return nil
	}
	return fmt.Errorf("could not find buildId in homepage")
}

func (f *FlameComics) ListSeries(ctx context.Context, client *httpclient.HTTPClient) ([]sources.Series, error) {
	f.Logger.Info("fetching series list from FlameComics")

	if err := f.fetchBuildID(ctx); err != nil {
		return nil, err
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/api/series", f.GetBaseURL()), nil)
	req.Header.Set("Origin", f.GetBaseURL())
	req.Header.Set("Referer", f.GetBaseURL()+"/")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch series list: %w", err)
	}
	defer resp.Body.Close()

	var seriesList []FlameComicsSeriesListItem
	if err := json.NewDecoder(resp.Body).Decode(&seriesList); err != nil {
		return nil, fmt.Errorf("failed to decode JSON: %w", err)
	}

	allSeries := make([]sources.Series, 0, len(seriesList))
	for _, item := range seriesList {
		allSeries = append(allSeries, sources.Series{
			Slug:   fmt.Sprintf("%d - %s", item.ID, item.Label),
			Title:  item.Label,
			Status: item.Status,
		})
	}

	f.Logger.Info("fetched series from FlameComics", "count", len(allSeries))
	return allSeries, nil
}

func (f *FlameComics) ScrapeComicChaptersURL(ctx context.Context, client *httpclient.HTTPClient, series sources.Series) ([]sources.Chapter, error) {
	f.Logger.Info("fetching chapters", "series", series.Slug)

	seriesID, err := f.extractSeriesID(series.Slug)
	if err != nil {
		return nil, err
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/_next/data/%s/series/%02d.json", f.GetBaseURL(), f.dataToken, seriesID), nil)
	req.Header.Set("Origin", f.GetBaseURL())
	req.Header.Set("Referer", f.GetBaseURL()+"/")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch chapters: %w", err)
	}
	defer resp.Body.Close()

	var apiResponse FlameComicsChaptersResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResponse); err != nil {
		return nil, fmt.Errorf("failed to decode JSON: %w", err)
	}

	return f.parseChapters(apiResponse.PageProps.Chapters, seriesID), nil
}

func (f *FlameComics) ScrapeChapterImagesURL(ctx context.Context, client *httpclient.HTTPClient, chapter sources.Chapter) ([]sources.Page, error) {
	f.Logger.Info("fetching pages", "chapter", chapter.Number)

	req, _ := http.NewRequestWithContext(ctx, "GET", chapter.URL, nil)
	req.Header.Set("Origin", f.GetBaseURL())
	req.Header.Set("Referer", f.GetBaseURL()+"/")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pages: %w", err)
	}
	defer resp.Body.Close()

	var apiResponse FlameComicsChapterResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResponse); err != nil {
		return nil, fmt.Errorf("failed to decode JSON: %w", err)
	}

	return f.parsePages(chapter.URL, apiResponse.PageProps.Chapter.Images), nil
}

func (f *FlameComics) parseChapters(flameChapters []FlameComicsChapter, seriesID int) []sources.Chapter {
	chapters := make([]sources.Chapter, 0, len(flameChapters))
	for _, ch := range flameChapters {
		chapterNum := ch.Chapter
		if chapterNum == "" {
			chapterNum = strconv.Itoa(ch.ChapterID)
		}

		chapterURL := fmt.Sprintf("%s/_next/data/%s/series/%02d/%s.json", f.GetBaseURL(), f.dataToken, seriesID, ch.Token)
		chapters = append(chapters, sources.Chapter{
			Number:    f.NormalizeChapterNumber(chapterNum),
			Title:     ch.Title,
			URL:       chapterURL,
			SourceURL: chapterURL,
		})
	}
	return chapters
}

func (f *FlameComics) parsePages(chapterURL string, images map[string]FlameImageInfo) []sources.Page {
	if len(images) == 0 {
		return []sources.Page{}
	}

	re := regexp.MustCompile(`/series/(\d+)/([^/]+)\.json`)
	if matches := re.FindStringSubmatch(chapterURL); len(matches) >= 3 {
		if seriesID, err := strconv.Atoi(matches[1]); err == nil {
			token := matches[2]
			pages := make([]sources.Page, 0, len(images))
			for i := 0; i < len(images); i++ {
				if imgInfo, ok := images[strconv.Itoa(i)]; ok {
					pages = append(pages, sources.Page{
						Number: i,
						URL:    fmt.Sprintf("https://cdn.flamecomics.xyz/uploads/images/series/%d/%s/%s", seriesID, token, imgInfo.Name),
					})
				}
			}
			return pages
		}
	}
	return []sources.Page{}
}

func (f *FlameComics) extractSeriesID(slug string) (int, error) {
	parts := strings.SplitN(slug, " - ", 2)
	if len(parts) < 1 {
		return 0, fmt.Errorf("invalid series slug format: %s", slug)
	}
	return strconv.Atoi(parts[0])
}
