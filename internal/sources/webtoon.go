package sources

import (
	"comicrawl/internal/httpclient"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// FIXME
// Webtoon has no decent way to get a list of all series on the platform
// this means that we are limited to effectively bruteforcing the series list
// which is relatively easy due to the series ids being sequential but will be ugly
type Webtoon struct {
	*BaseSource
	langCode string
}

func NewWebtoon(logger *slog.Logger) *Webtoon {
	return &Webtoon{
		BaseSource: NewBaseSource("webtoon", "https://www.webtoons.com", logger),
		langCode:   "en",
	}
}

// ExtractSlugFromURL overrides the base implementation with Webtoon-specific logic
func (w *Webtoon) ExtractSlugFromURL(urlStr string) (string, error) {
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

	// Special handling for Webtoon URLs which have format like /en/fantasy/tower-of-god/list
	// We want to extract "tower-of-god" not "list"
	if len(segments) >= 3 && (segments[0] == "en" || segments[0] == "fr" || segments[0] == "es" || segments[0] == "de" || segments[0] == "zh") {
		// Language code followed by category followed by series name
		if len(segments) >= 3 {
			// Filter out common suffixes like "list" and "series"
			seriesName := segments[2]
			if seriesName != "list" && seriesName != "series" {
				return seriesName, nil
			}
		}
	}

	// General case: take the last non-empty segment
	// Filter out common suffixes like "list" and "series"
	for i := len(segments) - 1; i >= 0; i-- {
		if segments[i] != "" && segments[i] != "list" && segments[i] != "series" {
			return segments[i], nil
		}
	}

	return "", fmt.Errorf("could not extract slug from URL: %s", urlStr)
}

// BuildURL overrides the base implementation with Webtoon-specific logic
func (w *Webtoon) BuildURL(path string) string {
	// For Webtoon, we need to include the language code in the path
	trimmedPath := strings.TrimLeft(path, "/")
	if trimmedPath == "" {
		return fmt.Sprintf("%s/%s", w.baseURL, w.langCode)
	}
	return fmt.Sprintf("%s/%s/%s", w.baseURL, w.langCode, trimmedPath)
}

func (w *Webtoon) ListSeries(ctx context.Context, client *httpclient.HTTPClient) ([]Series, error) {
	w.logger.Info("fetching series list from Webtoon")

	var allSeries []Series

	// Webtoon has different ranking categories
	rankings := []string{"trending", "popular", "originals", "canvas", "latest"}

	for _, ranking := range rankings {
		url := fmt.Sprintf("%s/%s/ranking/%s", w.BaseURL(), w.langCode, ranking)
		w.logger.Debug("fetching ranking page", "ranking", ranking, "url", url)

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Referer", w.BaseURL()+"/")

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch ranking page %s: %w", ranking, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		}

		doc, err := goquery.NewDocumentFromReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to parse HTML: %w", err)
		}

		pageSeries := w.parseSeriesPage(doc)
		allSeries = append(allSeries, pageSeries...)
	}

	// Remove duplicates based on slug
	uniqueSeries := w.removeDuplicateSeries(allSeries)

	w.logger.Info("fetched series from Webtoon", "count", len(uniqueSeries))
	return uniqueSeries, nil
}

func (w *Webtoon) parseSeriesPage(doc *goquery.Document) []Series {
	var series []Series

	doc.Find(".webtoon_list li a").Each(func(i int, s *goquery.Selection) {
		url, exists := s.Attr("href")
		if !exists {
			return
		}

		title := s.Find(".title").Text()

		slug, err := w.ExtractSlugFromURL(url)
		if err != nil {
			w.logger.Warn("failed to extract slug from URL", "url", url, "error", err)
			return
		}

		// Debug logging to see what series are being fetched
		w.logger.Info("found series", "title", title, "slug", slug, "url", url)

		if title != "" && slug != "" {
			series = append(series, Series{
				Slug:        slug,
				Title:       strings.TrimSpace(title),
				Description: "",         // Will be fetched in FetchChapters
				Author:      "",         // Will be fetched in FetchChapters
				Status:      "",         // Will be fetched in FetchChapters
				Genres:      []string{}, // Will be fetched in FetchChapters
			})
		}
	})

	return series
}

func (w *Webtoon) removeDuplicateSeries(series []Series) []Series {
	seen := make(map[string]bool)
	var unique []Series

	for _, s := range series {
		if !seen[s.Slug] {
			seen[s.Slug] = true
			unique = append(unique, s)
		}
	}

	return unique
}

func (w *Webtoon) FetchChapters(ctx context.Context, client *httpclient.HTTPClient, series Series) ([]Chapter, error) {
	w.logger.Info("fetching chapters", "series", series.Slug)

	// First fetch series details to get the title_no
	detailsURL := w.BuildURL(fmt.Sprintf("%s/list", series.Slug))
	req, err := http.NewRequestWithContext(ctx, "GET", detailsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Referer", w.BaseURL()+"/")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch series details: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	// Extract title_no from the page
	titleNo := w.extractTitleNo(doc, series.Slug)
	if titleNo == "" {
		return nil, fmt.Errorf("could not extract title_no for series %s", series.Slug)
	}

	// Determine if it's webtoon or canvas
	webtoonType := "webtoon"
	if strings.Contains(series.Slug, "canvas") {
		webtoonType = "canvas"
	}

	// Fetch chapters via API
	apiURL := fmt.Sprintf("https://m.webtoons.com/api/v1/%s/%s/episodes?pageSize=99999", webtoonType, titleNo)
	w.logger.Debug("fetching chapters via API", "url", apiURL)

	req, err = http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create API request: %w", err)
	}

	req.Header.Set("Referer", "https://m.webtoons.com/")

	resp, err = client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch chapters API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code from API: %d", resp.StatusCode)
	}

	var apiResponse EpisodeListResponse
	err = json.NewDecoder(resp.Body).Decode(&apiResponse)
	if err != nil {
		return nil, fmt.Errorf("failed to decode API response: %w", err)
	}

	chapters := w.parseChaptersFromAPI(apiResponse.Result.EpisodeList)
	w.logger.Info("parsed chapters", "series", series.Slug, "count", len(chapters))
	return chapters, nil
}

// extractTitleNo extracts the title number from the Webtoon series page
func (w *Webtoon) extractTitleNo(doc *goquery.Document, seriesSlug string) string {
	// Try to extract from URL parameters in the page
	var titleNo string
	doc.Find("a[href*='title_no='], a[href*='titleNo=']").Each(func(i int, s *goquery.Selection) {
		if titleNo != "" {
			return // Already found, skip
		}

		href, _ := s.Attr("href")
		if href != "" {
			parsed, err := url.Parse(href)
			if err == nil {
				if tn := parsed.Query().Get("title_no"); tn != "" {
					titleNo = tn
					return
				}
				if tn := parsed.Query().Get("titleNo"); tn != "" {
					titleNo = tn
					return
				}
			}
		}
	})

	if titleNo != "" {
		return titleNo
	}

	// Try to extract from data attributes
	titleNo, _ = doc.Find("[data-title-no]").Attr("data-title-no")
	if titleNo != "" {
		return titleNo
	}

	// Try to extract from script content
	scriptContent := doc.Find("script").Text()
	re := regexp.MustCompile(`titleNo[\s=:]+['\"](\d+)['\"]`)
	matches := re.FindStringSubmatch(scriptContent)
	if len(matches) > 1 {
		return matches[1]
	}

	// Fallback: try to extract from the slug or URL structure
	parts := strings.Split(seriesSlug, "-")
	for i := len(parts) - 1; i >= 0; i-- {
		if _, err := strconv.Atoi(parts[i]); err == nil {
			return parts[i]
		}
	}

	return ""
}

func (w *Webtoon) parseChaptersFromAPI(episodes []Episode) []Chapter {
	var chapters []Chapter

	// Process episodes to determine chapter numbers
	var recognized, unrecognized int
	episodeNoRegex := regexp.MustCompile(`(?i)(?:(s(eason)?|saison|part|vol(ume)?)\s*\.?\s*(\d+).*?)?(.*?(mini|bonus|special).*?)?(e(p(isode)?)?|ch(apter)?)\s*\.?\s*(\d+(\.\d+)?)`)

	for i := range episodes {
		episode := &episodes[i]
		matches := episodeNoRegex.FindStringSubmatch(episode.EpisodeTitle)

		if matches != nil && matches[6] == "" { // Skip mini/bonus episodes
			episode.ChapterNumber = matches[11]
			episode.SeasonNumber = 1
			if matches[4] != "" {
				if season, err := strconv.Atoi(matches[4]); err == nil {
					episode.SeasonNumber = season
				}
			}
			recognized++
		} else {
			episode.ChapterNumber = "-1"
			unrecognized++
		}
	}

	// Use sequential numbering if more episodes are unrecognized
	useSequential := unrecognized > recognized

	if useSequential {
		for i := range episodes {
			episodes[i].ChapterNumber = strconv.Itoa(i + 1)
		}
	} else {
		var maxChapterNumber float64
		currentSeason := 1
		var seasonOffset float64

		for i := range episodes {
			episode := &episodes[i]
			if episode.ChapterNumber != "-1" {
				originalNumber, err := strconv.ParseFloat(episode.ChapterNumber, 64)
				if err != nil {
					continue
				}

				// Check if we've moved to a new season
				if episode.SeasonNumber > currentSeason {
					currentSeason = episode.SeasonNumber
					if originalNumber <= maxChapterNumber {
						seasonOffset = maxChapterNumber
					}
				}

				episode.ChapterNumber = fmt.Sprintf("%.2f", seasonOffset+originalNumber)
				if num, _ := strconv.ParseFloat(episode.ChapterNumber, 64); num > maxChapterNumber {
					maxChapterNumber = num
				}
			} else {
				if i == 0 {
					episode.ChapterNumber = "0"
				} else {
					prevNum, _ := strconv.ParseFloat(episodes[i-1].ChapterNumber, 64)
					episode.ChapterNumber = fmt.Sprintf("%.2f", prevNum+0.01)
				}
			}
		}
	}

	// Create chapters in reverse order (newest first)
	for i := len(episodes) - 1; i >= 0; i-- {
		episode := episodes[i]

		chapterTitle := episode.EpisodeTitle
		if episode.ChapterNumber != "-1" {
			chapterTitle = fmt.Sprintf("%s (ch. %s)", chapterTitle, episode.ChapterNumber)
		}
		if episode.HasBGM {
			chapterTitle += " ♫"
		}

		chapters = append(chapters, Chapter{
			Number:    w.NormalizeChapterNumber(episode.ChapterNumber),
			Title:     chapterTitle,
			URL:       episode.ViewerLink,
			SourceURL: episode.ViewerLink,
		})
	}

	return chapters
}

func (w *Webtoon) FetchPages(ctx context.Context, client *httpclient.HTTPClient, chapter Chapter) ([]Page, error) {
	w.logger.Info("fetching pages", "chapter", chapter.Number)

	req, err := http.NewRequestWithContext(ctx, "GET", chapter.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Referer", w.BaseURL()+"/")

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

	return w.parsePages(doc)
}

func (w *Webtoon) parsePages(doc *goquery.Document) ([]Page, error) {
	var pages []Page

	// Try to find regular image pages first
	doc.Find("div#_imageList > img").Each(func(i int, s *goquery.Selection) {
		imageURL, _ := s.Attr("data-url")
		if imageURL != "" {
			// Remove quality parameter for max quality
			parsed, err := url.Parse(imageURL)
			if err == nil && parsed.Query().Get("type") == "q90" {
				query := parsed.Query()
				query.Del("type")
				parsed.RawQuery = query.Encode()
				imageURL = parsed.String()
			}

			pages = append(pages, Page{
				Number: len(pages),
				URL:    imageURL,
			})
		}
	})

	// If no regular images found, try motion toon pages
	if len(pages) == 0 {
		motionPages, err := w.parseMotionToonPages(doc)
		if err != nil {
			w.logger.Warn("failed to parse motion toon pages", "error", err)
		} else {
			pages = append(pages, motionPages...)
		}
	}

	w.logger.Info("parsed pages", "count", len(pages))
	return pages, nil
}

func (w *Webtoon) parseMotionToonPages(doc *goquery.Document) ([]Page, error) {
	docString, err := doc.Html()
	if err != nil {
		return nil, err
	}

	docURLRegex := regexp.MustCompile(`documentURL:[^']*'([^']+)'`)
	motionToonPathRegex := regexp.MustCompile(`jpg:[^']*'([^']+)\{`)

	docURLMatch := docURLRegex.FindStringSubmatch(docString)
	motionToonPathMatch := motionToonPathRegex.FindStringSubmatch(docString)

	if docURLMatch == nil || motionToonPathMatch == nil {
		return nil, fmt.Errorf("motion toon data not found")
	}

	docURL := docURLMatch[1]
	motionToonPath := motionToonPathMatch[1]

	// Note: This would require making another HTTP request to fetch motion toon data
	// For now, we'll return an empty slice as this is a more complex feature
	w.logger.Debug("motion toon detected but not implemented", "docURL", docURL, "path", motionToonPath)
	return []Page{}, nil
}

// API Response Structures
type EpisodeListResponse struct {
	Result struct {
		EpisodeList []Episode `json:"episodeList"`
	} `json:"result"`
}

type Episode struct {
	EpisodeTitle       string `json:"episodeTitle"`
	ViewerLink         string `json:"viewerLink"`
	ExposureDateMillis int64  `json:"exposureDateMillis"`
	HasBGM             bool   `json:"hasBGM"`
	ChapterNumber      string `json:"-"` // Not in JSON, we'll calculate this
	SeasonNumber       int    `json:"-"` // Not in JSON, we'll calculate this
}
