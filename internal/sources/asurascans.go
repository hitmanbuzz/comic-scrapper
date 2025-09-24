package sources

import (
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
	*BaseSource
}

func NewAsuraScans(logger *slog.Logger) *AsuraScans {
	return &AsuraScans{
		BaseSource: NewBaseSource("asurascans", "https://asuracomic.net", logger),
	}
}

func (a *AsuraScans) ListSeries(ctx context.Context, client *http.Client) ([]Series, error) {
	a.logger.Info("fetching series list from AsuraScans")
	
	var allSeries []Series
	page := 1
	
	for {
		url := fmt.Sprintf("%s/series?genres=&status=-1&types=-1&order=rating&page=%d", a.BaseURL(), page)
		a.logger.Debug("fetching series page", "page", page, "url", url)
		
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch series page %d: %w", page, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		}

		doc, err := goquery.NewDocumentFromReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to parse HTML: %w", err)
		}

		pageSeries := a.parseSeriesPage(doc)
		allSeries = append(allSeries, pageSeries...)
		
		if !a.hasNextPage(doc) {
			break
		}
		
		page++
	}

	a.logger.Info("fetched series from AsuraScans", "count", len(allSeries))
	return allSeries, nil
}

func (a *AsuraScans) parseSeriesPage(doc *goquery.Document) []Series {
	var series []Series
	
	doc.Find("div.grid > a[href]").Each(func(i int, s *goquery.Selection) {
		url, exists := s.Attr("href")
		if !exists {
			return
		}
		
		title := s.Find("div.block > span.block").First().Text()
		if title == "" {
			title = s.Find("span.block").First().Text()
		}
		
		slug, err := a.ExtractSlugFromURL(url)
		if err != nil {
			a.logger.Warn("failed to extract slug from URL", "url", url, "error", err)
			return
		}
		
		if title != "" && slug != "" {
			series = append(series, Series{
				Slug:  slug,
				Title: strings.TrimSpace(title),
			})
		}
	})
	
	return series
}

func (a *AsuraScans) hasNextPage(doc *goquery.Document) bool {
	return doc.Find("div.flex > a.flex.bg-themecolor:contains(Next)").Length() > 0
}

func (a *AsuraScans) FetchChapters(ctx context.Context, client *http.Client, series Series) ([]Chapter, error) {
	a.logger.Info("fetching chapters", "series", series.Slug)
	
	// Build series URL
	url := fmt.Sprintf("%s/series/%s", a.BaseURL(), series.Slug)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
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

	return a.parseChaptersPage(doc, series.Slug)
}

func (a *AsuraScans) parseChaptersPage(doc *goquery.Document, seriesSlug string) ([]Chapter, error) {
	var chapters []Chapter
	
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
		
		chapters = append(chapters, Chapter{
			Number:    a.NormalizeChapterNumber(chapterNumber),
			Title:     strings.TrimSpace(chapterTitle),
			URL:       a.ensureAbsoluteURL(url),
			SourceURL: a.ensureAbsoluteURL(url),
		})
	})

	a.logger.Info("parsed chapters", "series", seriesSlug, "count", len(chapters))
	return chapters, nil
}

func (a *AsuraScans) FetchPages(ctx context.Context, client *http.Client, chapter Chapter) ([]Page, error) {
	a.logger.Info("fetching pages", "chapter", chapter.Number)
	
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

func (a *AsuraScans) parsePages(doc *goquery.Document) ([]Page, error) {
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
        return nil, fmt.Errorf("failed to parse pages JSON: %v", err)
    }
    
    // Sort by order and create Page objects
    sort.Slice(pageData, func(i, j int) bool {
        return pageData[i].Order < pageData[j].Order
    })
    
    pages := make([]Page, len(pageData))
    for i, data := range pageData {
        pages[i] = Page{
						Number: i,
          	URL:   data.URL,
        }
    }
    
    a.logger.Info("parsed pages", "count", len(pages))
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
		return a.BaseURL() + url
	}

	if strings.Contains(url, "/chapter/") {
		// Normalize series URL by replacing hash with static 'aaaaaaaa'
		parts := strings.Split(strings.TrimSuffix(url, "/"), "/")
		if len(parts) >= 3 && len(parts[len(parts)-1]) == 8 {
			parts[len(parts)-1] = "aaaaaaaa"
		}
		return a.BaseURL() + "/series/" + strings.Join(parts, "/") + "/"
	}
	
	return a.BaseURL() + "/" + url
}