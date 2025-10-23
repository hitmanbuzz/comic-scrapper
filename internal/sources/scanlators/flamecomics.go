package scanlators

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"comicrawl/internal/httpclient"
	"comicrawl/internal/sources"
)

type FlameComics struct {
	*sources.BaseSource
	dataToken string // The N0ZxdmTTKvcQl_UJTlBzf token
}

func NewFlameComics(logger *slog.Logger) *FlameComics {
	return &FlameComics{
		BaseSource: sources.NewBaseSource("flamecomics", "https://flamecomics.xyz", logger),
		dataToken:  "N0ZxdmTTKvcQl_UJTlBzf", // May need to be updated if it changes
	}
}

// sanitizeTitle converts a series title to a filesystem-safe folder name
func sanitizeTitle(title string) string {
	// Remove or replace characters that are problematic in filenames
	// Replace common problematic characters with safe alternatives
	title = strings.ReplaceAll(title, "/", "-")
	title = strings.ReplaceAll(title, "\\", "-")
	title = strings.ReplaceAll(title, ":", " -")
	title = strings.ReplaceAll(title, "*", "")
	title = strings.ReplaceAll(title, "?", "")
	title = strings.ReplaceAll(title, "\"", "'")
	title = strings.ReplaceAll(title, "<", "")
	title = strings.ReplaceAll(title, ">", "")
	title = strings.ReplaceAll(title, "|", "-")
	
	// Remove multiple spaces and trim
	re := regexp.MustCompile(`\s+`)
	title = re.ReplaceAllString(title, " ")
	title = strings.TrimSpace(title)
	
	// Limit length to avoid filesystem issues
	if len(title) > 200 {
		title = title[:200]
	}
	
	return title
}

// FlameComics API Response Structures
type FlameComicsSeriesResponse struct {
	PageProps struct {
		Series   FlameComicsSeries    `json:"series"`
		Chapters []FlameComicsChapter `json:"chapters"`
	} `json:"pageProps"`
}

type FlameComicsSeries struct {
	ID          int                  `json:"series_id"`
	Title       string               `json:"title"`
	AltTitles   []string             `json:"altTitles"`
	Description string               `json:"description"`
	Author      []string             `json:"author"`
	Artist      []string             `json:"artist"`
	Status      string               `json:"status"`
	Tags        []string             `json:"tags"`
}

// FlexibleString handles JSON fields that can be either string or array of strings
type FlexibleString string

func (fs *FlexibleString) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as string first
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*fs = FlexibleString(s)
		return nil
	}

	// If that fails, try as array of strings
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		if len(arr) > 0 {
			*fs = FlexibleString(strings.Join(arr, ", "))
		} else {
			*fs = ""
		}
		return nil
	}

	// If both fail, set to empty string
	*fs = ""
	return nil
}

type FlameComicsChapter struct {
	ChapterID int                       `json:"chapter_id"`
	SeriesID  int                       `json:"series_id"`
	Chapter   string                    `json:"chapter"`
	Title     string                    `json:"title"`
	Token     string                    `json:"token"`
	Images    map[string]FlameImageInfo `json:"images"`
}

type FlameImageInfo struct {
	Name string `json:"name"`
}

func (f *FlameComics) ListSeries(ctx context.Context, client *httpclient.HTTPClient) ([]sources.Series, error) {
	f.Logger.Info("fetching series list from FlameComics")

	var allSeries []sources.Series
	seriesNum := 1
	consecutive404s := 0
	max404s := 50 // Stop after 50 consecutive 404s (no more series)

	// Bruteforce series numbers starting from 01
	for {
		// Format with leading zero for API (01, 02, 03, etc.)
		seriesNumStr := fmt.Sprintf("%02d", seriesNum)
		
		url := fmt.Sprintf("%s/_next/data/%s/series/%s.json", f.GetBaseURL(), f.dataToken, seriesNumStr)
		f.Logger.Debug("fetching series", "series_num", seriesNumStr, "url", url)

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Accept", "application/json")
		req.Header.Set("Referer", f.GetBaseURL()+"/")

		resp, err := client.Do(req)
		if err != nil {
			// Check if it's a 404 error from HTTPClient
			if strings.Contains(err.Error(), "404") {
				f.Logger.Debug("series not found", "series_num", seriesNumStr)
				consecutive404s++
				if consecutive404s >= max404s {
					f.Logger.Info("stopping after 50 consecutive 404s", "last_series_num", seriesNum-1)
					break
				}
			} else {
				f.Logger.Warn("failed to fetch series", "series_num", seriesNumStr, "error", err)
			}
			seriesNum++
			continue
		}

		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == 404 {
			resp.Body.Close()
			f.Logger.Debug("series not found", "series_num", seriesNumStr)
			consecutive404s++
			if consecutive404s >= max404s {
				f.Logger.Info("stopping after reaching the limit of 50 consecutive 404s", "last_series_num", seriesNum-1)
				break
			}
			seriesNum++
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			f.Logger.Warn("unexpected status code", "series_num", seriesNumStr, "status", resp.StatusCode)
			// Other status codes don't count as 404s, just skip
			seriesNum++
			continue
		}

		var apiResponse FlameComicsSeriesResponse
		err = json.NewDecoder(resp.Body).Decode(&apiResponse)
		resp.Body.Close()

		if err != nil {
			f.Logger.Warn("failed to decode JSON", "series_num", seriesNumStr, "error", err)
			// JSON decode errors don't count as 404s, just skip
			seriesNum++
			continue
		}

		// Successfully found a series, reset 404 counter
		consecutive404s = 0

		series := apiResponse.PageProps.Series
		if series.Title != "" {
			// Use sanitized title as slug for human-readable folder names
			// Format: "ID - Title" so we can extract ID later for API calls
			sanitizedTitle := sanitizeTitle(series.Title)
			slug := fmt.Sprintf("%d - %s", series.ID, sanitizedTitle)
			
			author := ""
			if len(series.Author) > 0 {
				author = strings.Join(series.Author, ", ")
			}

			allSeries = append(allSeries, sources.Series{
				Slug:        slug,
				Title:       series.Title,
				Description: series.Description,
				Author:      author,
				Status:      series.Status,
				Genres:      series.Tags,
			})

			f.Logger.Info("found series",
				"id", series.ID,
				"title", series.Title,
				"slug", slug)
		}

		seriesNum++

		// Safety limit to prevent infinite loops
		if seriesNum > 10000 {
			f.Logger.Warn("reached safety limit of 10000 series")
			break
		}
	}

	f.Logger.Info("fetched series from FlameComics", "count", len(allSeries))
	return allSeries, nil
}

func (f *FlameComics) FetchChapters(ctx context.Context, client *httpclient.HTTPClient, series sources.Series) ([]sources.Chapter, error) {
	f.Logger.Info("fetching chapters", "series", series.Slug)

	// Extract series ID from slug format "ID - Title"
	parts := strings.SplitN(series.Slug, " - ", 2)
	if len(parts) < 1 {
		return nil, fmt.Errorf("invalid series slug format: %s", series.Slug)
	}
	
	seriesID, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid series ID in slug: %s", series.Slug)
	}
	seriesNumStr := fmt.Sprintf("%02d", seriesID)

	url := fmt.Sprintf("%s/_next/data/%s/series/%s.json", f.GetBaseURL(), f.dataToken, seriesNumStr)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Referer", f.GetBaseURL()+"/")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch series data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var apiResponse FlameComicsSeriesResponse
	err = json.NewDecoder(resp.Body).Decode(&apiResponse)
	if err != nil {
		return nil, fmt.Errorf("failed to decode JSON: %w", err)
	}

	chapters := f.parseChapters(apiResponse.PageProps.Chapters, series.Slug)
	f.Logger.Info("parsed chapters", "series", series.Slug, "count", len(chapters))
	return chapters, nil
}

func (f *FlameComics) parseChapters(flameChapters []FlameComicsChapter, seriesSlug string) []sources.Chapter {
	var chapters []sources.Chapter

	// Extract just the ID from the slug format "ID - Title"
	parts := strings.SplitN(seriesSlug, " - ", 2)
	seriesID := parts[0] // Just the numeric ID

	for _, ch := range flameChapters {
		chapterNumber := ch.Chapter
		if chapterNumber == "" {
			chapterNumber = strconv.Itoa(ch.ChapterID)
		}

		// Extract image names from the map
		var imageNames []string
		for i := 0; i < len(ch.Images); i++ {
			if imgInfo, ok := ch.Images[strconv.Itoa(i)]; ok {
				imageNames = append(imageNames, imgInfo.Name)
			}
		}

		// Store the token and image list in the chapter URL as a special format
		// Use only the numeric ID (not the full "ID - Title" slug) for the URL
		chapterURL := fmt.Sprintf("flamecomics://%s/%s/%s", seriesID, ch.Token, strings.Join(imageNames, ","))

		chapters = append(chapters, sources.Chapter{
			Number:    f.NormalizeChapterNumber(chapterNumber),
			Title:     strings.TrimSpace(ch.Title),
			URL:       chapterURL,
			SourceURL: chapterURL,
		})
	}

	return chapters
}

func (f *FlameComics) FetchPages(ctx context.Context, client *httpclient.HTTPClient, chapter sources.Chapter) ([]sources.Page, error) {
	f.Logger.Info("fetching pages", "chapter", chapter.Number)

	// Parse the special URL format: flamecomics://seriesSlug/token/image1,image2,image3
	if !strings.HasPrefix(chapter.URL, "flamecomics://") {
		return nil, fmt.Errorf("invalid chapter URL format: %s", chapter.URL)
	}

	urlParts := strings.TrimPrefix(chapter.URL, "flamecomics://")
	parts := strings.SplitN(urlParts, "/", 3)
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid chapter URL format: %s", chapter.URL)
	}

	seriesSlug := parts[0]
	token := parts[1]
	imagesStr := parts[2]

	if imagesStr == "" {
		f.Logger.Warn("no images found for chapter", "chapter", chapter.Number)
		return []sources.Page{}, nil
	}

	images := strings.Split(imagesStr, ",")
	
	// Convert series slug to integer (without leading zero for CDN URL)
	seriesID, err := strconv.Atoi(seriesSlug)
	if err != nil {
		return nil, fmt.Errorf("invalid series slug: %s", seriesSlug)
	}

	var pages []sources.Page
	for i, imageName := range images {
		if imageName == "" {
			continue
		}

		// Build CDN URL: https://cdn.flamecomics.xyz/uploads/images/series/<series_number>/<token>/<image_name>
		// Note: series_number here is WITHOUT leading zero (1, 2, 3, not 01, 02, 03)
		imageURL := fmt.Sprintf("https://cdn.flamecomics.xyz/uploads/images/series/%d/%s/%s",
			seriesID, token, imageName)

		pages = append(pages, sources.Page{
			Number: i,
			URL:    imageURL,
		})
	}

	f.Logger.Info("parsed pages", "count", len(pages))
	return pages, nil
}
