package scanlators

import (
	"comicrawl/internal/cstructs/scrape_data"
	"comicrawl/internal/httpclient"
	"comicrawl/internal/sources"
	"comicrawl/internal/util"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type Webtoon struct {
	*sources.BaseSource
	langCode string
}

func NewWebtoon(logger *slog.Logger) *Webtoon {
	return &Webtoon{
		BaseSource: sources.NewBaseSource("webtoon", "https://www.webtoons.com", util.ParseSlugsToIds(util.Webtoon), logger),
		langCode:   "en",
	}
}

func (w *Webtoon) ListSeries(ctx context.Context, client *httpclient.HTTPClient) (scrape_data.FullSeriesResponse, error) {
	w.Logger.Info("fetching series list from Webtoon")

	var allSeries scrape_data.FullSeriesResponse

	genres := []string{
		"drama",
		"fantasy",
		"comedy",
		"action",
		"slice_of_life",
		"romance",
		"super_hero",
		"sf",
		"thriller",
		"supernatural",
		"mystery",
		"sports",
		"historical",
		"heartwarming",
		"horror",
		"graphic_novel",
		"tiptoon",
	}

	var pageSeries []sources.Series
	for _, genre := range genres {
		url := fmt.Sprintf("%s/%s/genres/%s", w.GetBaseURL(), w.langCode, genre)
		w.Logger.Debug("fetching ranking page", "genre", genre, "url", url)

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return allSeries, fmt.Errorf("failed to create request: %w", err)
		}

		// req.Header.Set("Origin", w.GetBaseURL())
		req.Header.Set("Referer", w.GetBaseURL())

		resp, err := client.Do(req)
		if err != nil {
			return allSeries, fmt.Errorf("failed to fetch ranking page %s: %w", genre, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return allSeries, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		}

		doc, err := goquery.NewDocumentFromReader(resp.Body)
		if err != nil {
			return allSeries, fmt.Errorf("failed to parse HTML: %w", err)
		}

		pageSeries = append(pageSeries, w.parseSeriesPage(doc)...)
	}

	// Remove duplicates based on URL
	uniqueMap := make(map[string]sources.Series)
	for _, s := range pageSeries {
		if _, exists := uniqueMap[s.URL]; !exists {
			uniqueMap[s.URL] = s
		}
	}

	allSeries.GroupName = w.GetName()
	allSeries.MuGroupIds = util.ParseSlugsToIds(util.Webtoon)

	for _, s := range uniqueMap {
		allSeries.Series = append(allSeries.Series, scrape_data.ScanSeriesResponse{
			MainTitle:    s.Title,
			ComicPageUrl: s.URL,
			MuSeriesId:   -1,
			ComicStatus:  s.Status,
			Found:        false,
		})
	}

	allSeries.TotalSeries = len(allSeries.Series)

	return allSeries, nil
}

func (w *Webtoon) parseSeriesPage(doc *goquery.Document) []sources.Series {
	var series []sources.Series

	doc.Find(".webtoon_list li a").Each(func(i int, s *goquery.Selection) {
		url, exists := s.Attr("href")
		if !exists {
			return
		}

		title := s.Find(".title").Text()
		if title != "" && url != "" {
			series = append(series, sources.Series{
				URL:    url,
				Title:  strings.TrimSpace(title),
			})
		}
	})

	return series
}

func (w *Webtoon) FetchChapters(ctx context.Context, client *httpclient.HTTPClient, series sources.Series) ([]sources.Chapter, error) {
	u, _ := url.Parse(series.URL)
	title_no := u.Query().Get("title_no")
	listIndex := strings.LastIndex(series.URL, "list?")
	seriesURL := series.URL[:listIndex]
	goodURL := fmt.Sprintf("%s/abc/viewer?title_no=%s&episode_no=1", seriesURL, title_no)
	
	req, err := http.NewRequestWithContext(ctx, "GET", goodURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Referer", w.GetBaseURL())

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

	return w.parseSeriesChapter(doc)
}

func (w *Webtoon) FetchPages(ctx context.Context, client *httpclient.HTTPClient, chapter sources.Chapter) ([]sources.Page, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", chapter.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Referer", w.GetBaseURL())

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

func (w *Webtoon) parseSeriesChapter(doc *goquery.Document) ([]sources.Chapter, error) {
	var chapters []sources.Chapter

	doc.Find(".episode_cont li a").Each(func(_ int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if exists {
			u, _ := url.Parse(href)
			episodeNum := u.Query().Get("episode_no")
			chapters = append(chapters, sources.Chapter{
				Number: util.StringToFloat(episodeNum),
				URL: href,
			})
		}
	})

	return chapters, nil
}

func (w *Webtoon) parsePages(doc *goquery.Document) ([]sources.Page, error) {
	var pages []sources.Page
	
	doc.Find(".viewer_img._img_viewer_area img").Each(func(i int, s *goquery.Selection) {
		dataUrl, exist := s.Attr("data-url")
		if exist {
			pages = append(pages, sources.Page{
				Number: i,
				URL: dataUrl,
			})
		}
	})

	return pages, nil
}
