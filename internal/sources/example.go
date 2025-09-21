package sources

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// ExampleSource implements a sample manga source using goquery
type ExampleSource struct {
	*BaseSource
}

func NewExampleSource(logger *slog.Logger) *ExampleSource {
	return &ExampleSource{
		BaseSource: NewBaseSource("example", "https://example-manga-site.com", logger),
	}
}

func (e *ExampleSource) ListSeries(ctx context.Context, client *http.Client) ([]Series, error) {
	e.logger.Info("fetching series list", "source", e.Name())
	
	// Example implementation - would need to be adapted for actual site structure
	req, err := http.NewRequestWithContext(ctx, "GET", e.BuildURL("/manga-list"), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch series list: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Parse HTML with goquery
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	var seriesList []Series
	
	// Example selector - would need to be adjusted for actual site
	doc.Find(".manga-item").Each(func(i int, s *goquery.Selection) {
		title := s.Find(".title").Text()
		url, _ := s.Find("a").Attr("href")
		
		if title != "" && url != "" {
			slug, err := e.ExtractSlugFromURL(url)
			if err != nil {
				e.logger.Warn("failed to extract slug from URL", "url", url, "error", err)
				return
			}
			
			seriesList = append(seriesList, Series{
				Slug:  slug,
				Title: strings.TrimSpace(title),
			})
		}
	})

	e.logger.Info("fetched series list", "count", len(seriesList), "source", e.Name())
	return seriesList, nil
}

func (e *ExampleSource) FetchChapters(ctx context.Context, client *http.Client, series Series) ([]Chapter, error) {
	e.logger.Info("fetching chapters", "series", series.Slug, "source", e.Name())
	
	// Build URL for series page
	url := e.BuildURL(fmt.Sprintf("/manga/%s", series.Slug))
	
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

	var chapters []Chapter
	
	// Example chapter parsing - would need to be adjusted for actual site
	doc.Find(".chapter-item").Each(func(i int, s *goquery.Selection) {
		chapterText := s.Find(".chapter-number").Text()
		chapterTitle := s.Find(".chapter-title").Text()
		chapterURL, _ := s.Find("a").Attr("href")
		
		if chapterText != "" && chapterURL != "" {
			// Extract chapter number from text
			chapterNum := e.extractChapterNumber(chapterText)
			
			chapters = append(chapters, Chapter{
				Number:    e.NormalizeChapterNumber(chapterNum),
				Title:     strings.TrimSpace(chapterTitle),
				URL:       e.BuildURL(chapterURL),
				SourceURL: e.BuildURL(chapterURL),
			})
		}
	})

	e.logger.Info("fetched chapters", 
		"series", series.Slug, 
		"count", len(chapters), 
		"source", e.Name())
	
	return chapters, nil
}

func (e *ExampleSource) FetchPages(ctx context.Context, client *http.Client, chapter Chapter) ([]Page, error) {
	e.logger.Info("fetching pages", 
		"series", "[will be extracted]", 
		"chapter", chapter.Number, 
		"source", e.Name())

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

	var pages []Page
	
	// Example page parsing - would need to be adjusted for actual site
	doc.Find(".page-image").Each(func(i int, s *goquery.Selection) {
		imgURL, _ := s.Find("img").Attr("src")
		if imgURL != "" {
			pages = append(pages, Page{
				Number: i + 1,
				URL:    e.ensureAbsoluteURL(imgURL),
			})
		}
	})

	e.logger.Info("fetched pages", 
		"chapter", chapter.Number, 
		"count", len(pages), 
		"source", e.Name())
	
	return pages, nil
}

// Helper methods for the example source

func (e *ExampleSource) extractChapterNumber(text string) string {
	// Extract chapter number using regex
	re := regexp.MustCompile(`(?i)chapter[\s:]*(\d+(?:\.\d+)?)`)
	matches := re.FindStringSubmatch(text)
	if len(matches) > 1 {
		return matches[1]
	}
	
	// Fallback: try to extract any number
	re = regexp.MustCompile(`(\d+(?:\.\d+)?)`)
	matches = re.FindStringSubmatch(text)
	if len(matches) > 1 {
		return matches[1]
	}
	
	return "0"
}

func (e *ExampleSource) ensureAbsoluteURL(url string) string {
	if strings.HasPrefix(url, "http") {
		return url
	}
	if strings.HasPrefix(url, "//") {
		return "https:" + url
	}
	if strings.HasPrefix(url, "/") {
		return e.BuildURL(url)
	}
	return e.BuildURL("/" + url)
}