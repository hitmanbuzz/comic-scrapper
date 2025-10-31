package scanlators

import (
	"comicrawl/internal/httpclient"
	"comicrawl/internal/sources"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type RizzComic struct {
	*sources.BaseSource
	globalPrefix string
}

func NewRizzComic(logger *slog.Logger) *RizzComic {
	return &RizzComic{
		BaseSource:   sources.NewBaseSource("rizzcomic", "https://rizzfables.com", logger),
		globalPrefix: "",
	}
}

type comicResponse struct {
	Title string `json:"title"`
}

func (r *RizzComic) ListSeries(ctx context.Context, client *httpclient.HTTPClient) ([]sources.Series, error) {
	r.Logger.Info("fetching series list from RizzComic")

	// Fetch global prefix (only once!)
	if r.globalPrefix == "" {
		r.Logger.Info("fetching global prefix")
		prefix, err := r.fetchGlobalPrefix(ctx, client)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch global prefix: %w", err)
		}
		r.globalPrefix = prefix
		r.Logger.Info("cached global prefix", "prefix", prefix)
	}

	// Build form
	form := url.Values{
		"search_value": {""},
		"StatusValue":  {"all"},
		"TypeValue":    {"all"},
		"OrderValue":   {"Popular"},
		"page":         {"1"},
	}

	req, _ := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/Index/filter_series", r.GetBaseURL()), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("X-API-Request", "1")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch series: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var comics []comicResponse
	if err := json.NewDecoder(resp.Body).Decode(&comics); err != nil {
		return nil, fmt.Errorf("failed to decode JSON: %w", err)
	}

	allSeries := make([]sources.Series, 0, len(comics))
	for _, comic := range comics {
		allSeries = append(allSeries, sources.Series{
			Slug:  r.slugify(comic.Title),
			Title: comic.Title,
		})
	}

	r.Logger.Info("fetched series from RizzComic", "count", len(allSeries))
	return allSeries, nil
}

func (r *RizzComic) FetchChapters(ctx context.Context, client *httpclient.HTTPClient, series sources.Series) ([]sources.Chapter, error) {
	r.Logger.Info("fetching chapters", "series", series.Slug)

	req, _ := http.NewRequestWithContext(ctx, "GET", r.BuildURL(fmt.Sprintf("series/%s%s", r.globalPrefix, series.Slug)), nil)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch chapters page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse chapters HTML: %w", err)
	}

	var chapters []sources.Chapter
	doc.Find("a").EachWithBreak(func(i int, s *goquery.Selection) bool {
		href, exists := s.Attr("href")
		if !exists || !strings.Contains(href, "/chapter/") {
			return true
		}

		chapters = append(chapters, sources.Chapter{
			Number:    r.extractChapterNumber(s.Text()),
			Title:     strings.TrimSpace(s.Text()),
			URL:       href,
			SourceURL: href,
		})
		return true
	})

	r.Logger.Info("parsed chapters", "series", series.Slug, "count", len(chapters))
	return chapters, nil
}

func (r *RizzComic) fetchGlobalPrefix(ctx context.Context, client *httpclient.HTTPClient) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/series/", r.GetBaseURL()), nil)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch series listing: %w", err)
	}
	defer resp.Body.Close()

	doc, _ := goquery.NewDocumentFromReader(resp.Body)

	var prefix string
	doc.Find("a").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if href, exists := s.Attr("href"); exists && strings.Contains(href, "/series/") {
			if parts := strings.Split(href, "/series/"); len(parts) == 2 {
				if matches := regexp.MustCompile(`^(r\d+)-`).FindStringSubmatch(strings.TrimSuffix(parts[1], "/")); len(matches) > 1 {
					prefix = matches[1] + "-"
					return false
				}
			}
		}
		return true
	})

	return prefix, nil
}

func (r *RizzComic) FetchPages(ctx context.Context, client *httpclient.HTTPClient, chapter sources.Chapter) ([]sources.Page, error) {
	r.Logger.Info("fetching pages", "chapter", chapter.Number)

	req, _ := http.NewRequestWithContext(ctx, "GET", chapter.URL, nil)
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

	return r.parsePages(doc)
}

func (r *RizzComic) parsePages(doc *goquery.Document) ([]sources.Page, error) {
	var pages []sources.Page

	doc.Find("img").Each(func(i int, s *goquery.Selection) {
		src, exists := s.Attr("src")
		if !exists || src == "" {
			return
		}

		// Make relative URLs absolute
		if !strings.HasPrefix(src, "http") {
			if strings.HasPrefix(src, "//") {
				src = "https:" + src
			} else {
				src = r.GetBaseURL() + src
			}
		}

		pages = append(pages, sources.Page{
			Number: i,
			URL:    src,
		})
	})

	r.Logger.Info("parsed pages", "count", len(pages))
	return pages, nil
}

func (r *RizzComic) extractChapterNumber(text string) string {
	re := regexp.MustCompile(`(?i)chapter[\s:]*(\d+(?:\.\d+)?)`)
	if matches := re.FindStringSubmatch(text); len(matches) > 1 {
		return matches[1]
	}
	return "0"
}

func (r *RizzComic) slugify(title string) string {
	re := regexp.MustCompile(`[^a-z0-9]+`)
	slug := re.ReplaceAllString(strings.ToLower(strings.ReplaceAll(title, "'", "")), "-")
	return strings.Trim(slug, "-")
}
