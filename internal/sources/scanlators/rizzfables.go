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
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type RizzFables struct {
	*sources.BaseSource
	globalPrefix string
}

func NewRizzFables(logger *slog.Logger) *RizzFables {
	return &RizzFables{
		BaseSource:   sources.NewBaseSource("rizzfables", "https://rizzfables.com", util.ParseSlugsToIds(util.RizzFables), logger),
		globalPrefix: "",
	}
}

type ComicResponse struct {
	Title  string `json:"title"`
	Status string `json:"status"`
}

func (r *RizzFables) ListSeries(ctx context.Context, client *httpclient.HTTPClient) (cstructs.FullSeriesResponse, error) {
	r.Logger.Info("fetching series list from RizzFables")

	var allSeries cstructs.FullSeriesResponse

	// Fetch global prefix (only once!)
	if r.globalPrefix == "" {
		r.Logger.Info("fetching global prefix")
		prefix, err := r.fetchGlobalPrefix(ctx, client)
		if err != nil {
			return allSeries, fmt.Errorf("failed to fetch global prefix: %w", err)
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
		return allSeries, fmt.Errorf("failed to fetch series: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return allSeries, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var comics []ComicResponse
	if err := json.NewDecoder(resp.Body).Decode(&comics); err != nil {
		return allSeries, fmt.Errorf("failed to decode JSON: %w", err)
	}

	allSeries.GroupName = r.GetName()
	allSeries.MuGroupIds = util.ParseSlugsToIds(util.RizzFables)
	allSeries.TotalSeries = len(comics)

	for _, comic := range comics {
		slug := r.slugify(comic.Title)
		seriesURL := r.BuildURL(fmt.Sprintf("series/%s%s", r.globalPrefix, slug))
		allSeries.Series = append(allSeries.Series, cstructs.ScanSeriesResponse{
			MainTitle:    comic.Title,
			ComicPageUrl: seriesURL,
			MuSeriesId:   -1,
			ComicStatus:  comic.Status, // Status not provided in response
			Found:        false,
		})
	}

	r.Logger.Info("fetched series from RizzFables", "count", len(allSeries.Series))
	return allSeries, nil
}

func (r *RizzFables) FetchChapters(ctx context.Context, client *httpclient.HTTPClient, series sources.Series) ([]sources.Chapter, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", series.URL, nil)
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

	return chapters, nil
}

func (r *RizzFables) fetchGlobalPrefix(ctx context.Context, client *httpclient.HTTPClient) (string, error) {
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

func (r *RizzFables) FetchPages(ctx context.Context, client *httpclient.HTTPClient, chapter sources.Chapter) ([]sources.Page, error) {
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

func (r *RizzFables) parsePages(doc *goquery.Document) ([]sources.Page, error) {
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

func (r *RizzFables) extractChapterNumber(text string) string {
	re := regexp.MustCompile(`(?i)chapter[\s:]*(\d+(?:\.\d+)?)`)
	if matches := re.FindStringSubmatch(text); len(matches) > 1 {
		return matches[1]
	}
	return "0"
}

func (r *RizzFables) slugify(title string) string {
	re := regexp.MustCompile(`[^a-z0-9]+`)
	slug := re.ReplaceAllString(strings.ToLower(strings.ReplaceAll(title, "'", "")), "-")
	return strings.Trim(slug, "-")
}
