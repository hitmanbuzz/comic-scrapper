package scanlators

import (
	"comicrawl/internal/cstructs/scrape_data"
	"comicrawl/internal/httpclient"
	"comicrawl/internal/sources"
	"comicrawl/internal/util"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type AsuraScans struct {
	*sources.BaseSource
}

func NewAsuraScans(logger *slog.Logger) *AsuraScans {
	return &AsuraScans{
		BaseSource: sources.NewBaseSource("asura", "https://asuracomic.net", util.ParseSlugsToIds(util.Asura), logger), // We can put those MU Group ID as a hardcoded value instead of parsing from slug
	}
}

func (a *AsuraScans) ListSeries(ctx context.Context, client *httpclient.HTTPClient) (scrape_data.FullSeriesResponse, error) {
	a.Logger.Info("fetching series list", "source", a.GetName())

	var allSeries scrape_data.FullSeriesResponse

	page := 1

	for {
		url := fmt.Sprintf("%s/series?genres=&status=-1&types=-1&order=rating&page=%d", a.GetBaseURL(), page)
		resp, err := sources.FetchWithContext(ctx, client, a.Logger, url, "fetching series page")
		if err != nil {
			return allSeries, err
		}

		doc, err := goquery.NewDocumentFromReader(resp.Body)
		if err != nil {
			return allSeries, fmt.Errorf("failed to parse HTML: %w", err)
		}

		pageSeries := a.parseSeriesData(doc)
		allSeries.GroupName = a.GetName()
		allSeries.MuGroupIds = util.ParseSlugsToIds(util.Asura)
		allSeries.TotalSeries = len(allSeries.Series)

		for _, data := range pageSeries {
			allSeries.Series = append(allSeries.Series, scrape_data.ScanSeriesResponse{
				MainTitle:    data.Title,
				ComicPageUrl: data.URL,
				MuSeriesId:   -1,
				ComicStatus:  data.Status,
				Found:        false,
			})
		}

		if !a.hasNextPage(doc) {
			break
		}

		page++
	}

	a.Logger.Info("fetched series from AsuraScans", "count", len(allSeries.Series))
	return allSeries, nil
}

func (a *AsuraScans) parseSeriesData(doc *goquery.Document) []sources.Series {
	var series []sources.Series

	doc.Find("div.grid > a[href]").Each(func(i int, s *goquery.Selection) {
		url, exists := s.Attr("href")
		if !exists {
			return
		}

		title := s.Find("div.block > span.block").First().Text()
		if title == "" {
			title = s.Find("span.block").First().Text()
		}

		status := s.Find("div.flex > span.status").First().Text()
		if status == "" {
			status = s.Find("span.status").First().Text()
		}

		status = strings.TrimSpace(strings.ToLower(status))

		switch status {
		case "completed", "ongoing", "hiatus":
		default:
			// Forecefully putting this status because status like `Season End` doesn't make sense to use
			status = "ongoing"
		}

		if title != "" && url != "" {
			series = append(series, sources.Series{
				URL:    fmt.Sprintf("%s/%s", a.BaseURL, url),
				Title:  strings.TrimSpace(title),
				Status: status,
			})
		}
	})

	return series
}

func (a *AsuraScans) hasNextPage(doc *goquery.Document) bool {
	return doc.Find("div.flex > a.flex.bg-themecolor:contains(Next)").Length() > 0
}

func (a *AsuraScans) FetchChapters(ctx context.Context, client *httpclient.HTTPClient, series sources.Series) ([]sources.Chapter, error) {
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

	return a.parseChaptersPage(doc)
}

func (a *AsuraScans) parseChaptersPage(doc *goquery.Document) ([]sources.Chapter, error) {
	var chapters []sources.Chapter

	doc.Find("div.scrollbar-thumb-themecolor > div.group:not(:has(svg))").Each(func(i int, s *goquery.Selection) {
		link := s.Find("a")
		url, exists := link.Attr("href")
		if !exists {
			return
		}

		chapterText := s.Find("h3").First().Text()
		chapterNumber := a.extractChapterNumber(chapterText)

		var titleParts []string
		s.Find("h3 > span").Each(func(j int, span *goquery.Selection) {
			if text := span.Text(); text != "" {
				titleParts = append(titleParts, text)
			}
		})

		chapterTitle := strings.Join(titleParts, " ")

		chapters = append(chapters, sources.Chapter{
			Number:    float32(util.StringToFloat(chapterNumber)),
			Title:     strings.TrimSpace(chapterTitle),
			URL:       a.ensureAbsoluteURL(url),
		})
	})

	return chapters, nil
}

func (a *AsuraScans) FetchPages(ctx context.Context, client *httpclient.HTTPClient, chapter sources.Chapter) ([]sources.Page, error) {
	a.Logger.Info("fetching pages", "chapter", chapter.Number)

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

	return a.parsePages(doc)
}

func (a *AsuraScans) parsePages(doc *goquery.Document) ([]sources.Page, error) {
	var scriptContent strings.Builder
	doc.Find("script").Each(func(i int, s *goquery.Selection) {
		content := s.Text()
		if strings.Contains(content, "self.__next_f.push") {
			start := strings.Index(content, "\"")
			end := strings.LastIndex(content, "\"")
			if start >= 0 && end > start {
				fragment := content[start+1 : end]
				scriptContent.WriteString(fragment)
			} else {
				scriptContent.WriteString(content)
			}
		}
	})

	combinedContent := scriptContent.String()

	re := regexp.MustCompile(`\\"pages\\":(\[.*?\])`)
	matches := re.FindStringSubmatch(combinedContent)

	if len(matches) < 2 {
		return nil, fmt.Errorf("pages array not found in script content")
	}

	pagesJSON := strings.ReplaceAll(matches[1], `\\`, `\`)
	pagesJSON = strings.ReplaceAll(pagesJSON, `\"`, `"`)

	var pageData []struct {
		Order int    `json:"order"`
		URL   string `json:"url"`
	}

	err := json.Unmarshal([]byte(pagesJSON), &pageData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse pages JSON: %w", err)
	}

	sort.Slice(pageData, func(i, j int) bool {
		return pageData[i].Order < pageData[j].Order
	})

	pages := make([]sources.Page, len(pageData))
	for i, data := range pageData {
		pages[i] = sources.Page{
			Number: i,
			URL:    data.URL,
		}
	}

	return pages, nil
}

func (a *AsuraScans) extractChapterNumber(text string) string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)chapter[\s:]*(\d+(?:\.\d+)?)`),
		regexp.MustCompile(`(\d+(?:\.\d+)?)`),
	}

	for _, re := range patterns {
		matches := re.FindStringSubmatch(text)
		if len(matches) > 1 {
			return matches[1]
		}
	}

	return "0"
}

func (a *AsuraScans) ensureAbsoluteURL(url string) string {
	if strings.HasPrefix(url, "http") {
		return url
	}
	if strings.HasPrefix(url, "//") {
		return "https:" + url
	}
	if strings.HasPrefix(url, "/") {
		return a.GetBaseURL() + url
	}

	if strings.Contains(url, "/chapter/") {
		parts := strings.Split(strings.TrimSuffix(url, "/"), "/")
		if len(parts) >= 3 && len(parts[len(parts)-1]) == 8 {
			parts[len(parts)-1] = "aaaaaaaa"
		}
		return a.GetBaseURL() + "/series/" + strings.Join(parts, "/") + "/"
	}

	return a.GetBaseURL() + "/" + url
}
