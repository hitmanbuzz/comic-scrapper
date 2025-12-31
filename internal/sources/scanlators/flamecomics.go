package scanlators

import (
	"comicrawl/internal/cstructs/scrape_data"
	"comicrawl/internal/httpclient"
	"comicrawl/internal/sources"
	"comicrawl/internal/util"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
)

type FlameComics struct {
	*sources.BaseSource
	buildID string
}

func NewFlameComics(logger *slog.Logger) *FlameComics {
	return &FlameComics{
		BaseSource: sources.NewBaseSource("flamecomics", "https://flamecomics.xyz", util.ParseSlugsToIds(util.FlameComics), logger),
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

// Extract BuildID from homepage
func (f *FlameComics) fetchBuildID(ctx context.Context, client *httpclient.HTTPClient, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch homepage: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	re := regexp.MustCompile(`"buildId":"([^"]+)"`)
	if matches := re.FindSubmatch(body); len(matches) >= 2 {
		id := string(matches[1])
		return id, nil
	}
	return "", fmt.Errorf("could not find buildId in homepage")
}

func (f *FlameComics) ListSeries(ctx context.Context, client *httpclient.HTTPClient) (scrape_data.FullSeriesResponse, error) {
	f.Logger.Info("fetching series list", "source", f.GetName())

	var allSeries scrape_data.FullSeriesResponse

	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/api/series", f.GetBaseURL()), nil)
	if err != nil {
		return allSeries, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Origin", f.GetBaseURL())
	req.Header.Set("Referer", f.GetBaseURL()+"/")

	resp, err := client.Do(req)
	if err != nil {
		return allSeries, fmt.Errorf("failed to fetch series list: %w", err)
	}
	defer resp.Body.Close()

	var seriesList []FlameComicsSeriesListItem
	if err := json.NewDecoder(resp.Body).Decode(&seriesList); err != nil {
		return allSeries, fmt.Errorf("failed to decode JSON: %w", err)
	}

	allSeries.GroupName = f.GetName()
	allSeries.MuGroupIds = util.ParseSlugsToIds(util.FlameComics)
	allSeries.TotalSeries = len(seriesList)

	for _, item := range seriesList {
		seriesURL := fmt.Sprintf("%s/series/%02d", f.GetBaseURL(), item.ID)
		allSeries.Series = append(allSeries.Series, scrape_data.ScanSeriesResponse{
			MainTitle:    item.Label,
			ComicPageUrl: seriesURL,
			MuSeriesId:   -1,
			ComicStatus:  item.Status,
			Found:        false,
		})
	}

	f.Logger.Info("fetched series from FlameComics", "count", len(allSeries.Series))
	return allSeries, nil
}

func (f *FlameComics) FetchChapters(ctx context.Context, client *httpclient.HTTPClient, series sources.Series) ([]sources.Chapter, error) {
	if f.buildID == "" {
		buildID, err := f.fetchBuildID(ctx, client, series.URL)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch build id for series: %s", series.URL)
		}

		f.buildID = buildID
	}
	
	seriesID, err := f.extractSeriesID(series.URL)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/_next/data/%s/series/%02d.json", f.GetBaseURL(), f.buildID, seriesID), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

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

func (f *FlameComics) FetchPages(ctx context.Context, client *httpclient.HTTPClient, chapter sources.Chapter) ([]sources.Page, error) {
	f.Logger.Info("fetching pages", "chapter", chapter.Number)

	req, err := http.NewRequestWithContext(ctx, "GET", chapter.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
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

		chapterURL := fmt.Sprintf("%s/_next/data/%s/series/%02d/%s.json", f.GetBaseURL(), f.buildID, seriesID, ch.Token)
		chapters = append(chapters, sources.Chapter{
			Number: util.StringToFloat(chapterNum),
			Title:  ch.Title,
			URL:    chapterURL,
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

func (f *FlameComics) extractSeriesID(seriesURL string) (int, error) {
	re := regexp.MustCompile(`/series/(\d+)`)
	matches := re.FindStringSubmatch(seriesURL)
	if len(matches) < 2 {
		return 0, fmt.Errorf("invalid series URL format: %s", seriesURL)
	}
	return strconv.Atoi(matches[1])
}
