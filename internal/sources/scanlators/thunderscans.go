package scanlators

import (
	"comicrawl/internal/cstructs"
	"comicrawl/internal/httpclient"
	"comicrawl/internal/sources"
	"comicrawl/internal/util"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type ThunderScans struct {
	*sources.BaseSource
}

func NewThunderScans(logger *slog.Logger) *ThunderScans {
	return &ThunderScans{
		BaseSource: sources.NewBaseSource("thunderscans", "https://en-thunderscans.com", util.ParseSlugsToIds(util.ThunderScans), logger),
	}
}

func (t *ThunderScans) ListSeries(ctx context.Context, client *httpclient.HTTPClient) (cstructs.FullSeriesResponse, error) {
	t.Logger.Info("fetching series list from ThunderScans")

	var allSeries cstructs.FullSeriesResponse

	url := fmt.Sprintf("%s/comics/list-mode/", t.GetBaseURL())
	resp, err := sources.FetchWithContext(ctx, client, t.Logger, url, "fetching series list")
	if err != nil {
		return allSeries, err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return allSeries, fmt.Errorf("failed to parse HTML: %w", err)
	}

	pageSeries := t.parseSeriesList(doc)
	allSeries.GroupName = t.GetName()
	allSeries.MuGroupIds = util.ParseSlugsToIds(util.ThunderScans)
	allSeries.TotalSeries = len(allSeries.Series)

	for _, data := range pageSeries {
		allSeries.Series = append(allSeries.Series, cstructs.ScanSeriesResponse{
			MainTitle:    data.Title,
			ComicPageUrl: data.URL,
			MuSeriesId:   -1,
			ComicStatus:  data.Status,
			Found:        false,
		})
	}

	t.Logger.Info("fetched series from ThunderScans", "count", len(allSeries.Series))
	return allSeries, nil
}

func (t *ThunderScans) parseSeriesList(doc *goquery.Document) []sources.Series {
	var series []sources.Series

	// Find all li elements containing a.series.tip links
	doc.Find("li a.series.tip").Each(func(i int, s *goquery.Selection) {
		url, exists := s.Attr("href")
		if !exists {
			return
		}

		title := strings.TrimSpace(s.Text())

		if title != "" && url != "" {
			series = append(series, sources.Series{
				URL:    url,
				Title:  title,
				Status: "", // Status not available on list page
			})
		}
	})

	return series
}

func (t *ThunderScans) FetchChapters(ctx context.Context, client *httpclient.HTTPClient, series sources.Series) ([]sources.Chapter, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", series.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch chapters: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	return t.parseChaptersPage(doc)
}

func (t *ThunderScans) parseChaptersPage(doc *goquery.Document) ([]sources.Chapter, error) {
	var chapters []sources.Chapter

	// MangaThemesia chapter list selector: div.bxcl li, div.cl li, #chapterlist li, ul li:has(div.chbox):has(div.eph-num)
	doc.Find("div.bxcl li, div.cl li, #chapterlist li, ul li:has(div.chbox):has(div.eph-num)").Each(func(i int, s *goquery.Selection) {
		link := s.Find("a")
		url, exists := link.Attr("href")
		if !exists {
			return
		}

		chapterText := link.Text()
		chapterNumber := t.extractChapterNumber(chapterText)

		var titleParts []string
		s.Find("span, .chapter-title").Each(func(j int, span *goquery.Selection) {
			if text := span.Text(); text != "" {
				titleParts = append(titleParts, text)
			}
		})

		chapterTitle := strings.Join(titleParts, " ")

		chapters = append(chapters, sources.Chapter{
			Number:    t.NormalizeChapterNumber(chapterNumber),
			Title:     strings.TrimSpace(chapterTitle),
			URL:       t.ensureAbsoluteURL(url),
			SourceURL: t.ensureAbsoluteURL(url),
		})
	})

	return chapters, nil
}

func (t *ThunderScans) FetchPages(ctx context.Context, client *httpclient.HTTPClient, chapter sources.Chapter) ([]sources.Page, error) {
	t.Logger.Info("fetching pages", "chapter", chapter.Number)

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

	return t.parsePages(doc)
}

func (t *ThunderScans) parsePages(doc *goquery.Document) ([]sources.Page, error) {
	var pages []sources.Page

	// MangaThemesia page selector: div#readerarea img, div#ts_reader img, div.ts_reader img
	doc.Find("div#readerarea img, div#ts_reader img, div.ts_reader img").Each(func(i int, s *goquery.Selection) {
		imageURL := s.AttrOr("src", "")
		if imageURL == "" {
			imageURL = s.AttrOr("data-src", "")
		}
		if imageURL == "" {
			imageURL = s.AttrOr("data-lazy-src", "")
		}

		// Transform Jetpack CDN URLs: remove the i[0-9].wp.com prefix
		imageURL = t.transformJetpackCDNURL(imageURL)

		if imageURL != "" {
			pages = append(pages, sources.Page{
				Number: i,
				URL:    imageURL,
			})
		}
	})

	// If no images found, try to parse from script content (JSON-based image loading)
	if len(pages) == 0 {
		pages = t.parsePagesFromScript(doc)
	}

	t.Logger.Info("parsed pages", "count", len(pages))
	return pages, nil
}

// transformJetpackCDNURL replaces Jetpack CDN URLs with direct HTTPS
// Example: https://i0.wp.com/example.com/image.jpg -> https://example.com/image.jpg
func (t *ThunderScans) transformJetpackCDNURL(url string) string {
	if url == "" {
		return url
	}

	// Match and replace i[0-9].wp.com prefix
	re := regexp.MustCompile(`^https://i[0-9]\.wp\.com/`)
	return re.ReplaceAllString(url, "https://")
}

// parsePagesFromScript attempts to extract page URLs from JavaScript content
// This handles sites that load images via JSON in script tags
func (t *ThunderScans) parsePagesFromScript(doc *goquery.Document) []sources.Page {
	var pages []sources.Page

	// Look for JSON data in script tags
	var scriptContent strings.Builder
	doc.Find("script").Each(func(i int, s *goquery.Selection) {
		content := s.Text()
		if strings.Contains(content, "images") {
			scriptContent.WriteString(content)
		}
	})

	combinedContent := scriptContent.String()
	if combinedContent == "" {
		return pages
	}

	// Try to find JSON array with image URLs
	// Pattern: "images":[...] or pages":[...]
	re1 := regexp.MustCompile(`"images"\s*:\s*(\[.*?\])`)
	matches1 := re1.FindStringSubmatch(combinedContent)

	re2 := regexp.MustCompile(`"pages"\s*:\s*(\[.*?\])`)
	matches2 := re2.FindStringSubmatch(combinedContent)

	matches := matches1
	if len(matches) < 2 && len(matches2) >= 2 {
		matches = matches2
	}

	if len(matches) >= 2 {
		pagesJSON := matches[1]
		// Extract URLs from JSON array
		urlRegex := regexp.MustCompile(`"([^"]+\.(?:jpg|jpeg|png|webp|gif))"`)
		urlMatches := urlRegex.FindAllStringSubmatch(pagesJSON, -1)

		for i, match := range urlMatches {
			if len(match) > 1 {
				url := match[1]
				url = t.transformJetpackCDNURL(url)
				pages = append(pages, sources.Page{
					Number: i,
					URL:    url,
				})
			}
		}
	}

	return pages
}

func (t *ThunderScans) extractChapterNumber(text string) string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)Chapter[\s:]*(\d+(?:\.\d+)?)`),
		regexp.MustCompile(`(?i)Ch\.\s*(\d+(?:\.\d+)?)`),
		regexp.MustCompile(`(?i)Ep(isode)?[\s:]*(\d+(?:\.\d+)?)`),
		regexp.MustCompile(`(\d+(?:\.\d+)?)`),
	}

	for _, re := range patterns {
		matches := re.FindStringSubmatch(text)
		if len(matches) > 1 {
			return matches[len(matches)-1]
		}
	}

	return "0"
}

func (t *ThunderScans) ensureAbsoluteURL(url string) string {
	if strings.HasPrefix(url, "http") {
		return url
	}
	if strings.HasPrefix(url, "//") {
		return "https:" + url
	}
	if strings.HasPrefix(url, "/") {
		return t.GetBaseURL() + url
	}

	return t.GetBaseURL() + "/" + url
}
