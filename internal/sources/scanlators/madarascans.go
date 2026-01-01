package scanlators

import (
	"comicrawl/internal/cstructs/scrape_data"
	"comicrawl/internal/httpclient"
	"comicrawl/internal/sources"
	"comicrawl/internal/util"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type MadaraScans struct {
	*sources.BaseSource
}

func NewMadaraScans(logger *slog.Logger) *MadaraScans {
	return &MadaraScans{
		BaseSource: sources.NewBaseSource("madarascans", "https://madarascans.com", util.ParseSlugsToIds(util.MadaraScans), logger),
	}
}

func (m *MadaraScans) ListSeries(ctx context.Context, client *httpclient.HTTPClient) (scrape_data.FullSeriesResponse, error) {
	m.Logger.Info("fetching series list", "source", m.GetName())

	var allSeries scrape_data.FullSeriesResponse

	url := fmt.Sprintf("%s/series/list-mode/", m.GetBaseURL())
	resp, err := sources.FetchWithContext(ctx, client, m.Logger, url, "fetching series list")
	if err != nil {
		return allSeries, err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return allSeries, fmt.Errorf("failed to parse HTML: %w", err)
	}

	pageSeries := m.parseSeriesList(doc)
	allSeries.GroupName = m.GetName()
	allSeries.MuGroupIds = util.ParseSlugsToIds(util.MadaraScans)
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

	return allSeries, nil
}

func (m *MadaraScans) parseSeriesList(doc *goquery.Document) []sources.Series {
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
				Status: "",
			})
		}
	})

	return series
}

func (m *MadaraScans) FetchChapters(ctx context.Context, client *httpclient.HTTPClient, series sources.Series) ([]sources.Chapter, error) {
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

	return m.parseChaptersPage(doc)
}

func (m *MadaraScans) parseChaptersPage(doc *goquery.Document) ([]sources.Chapter, error) {
	var chapters []sources.Chapter

	doc.Find("div.bxcl li, div.cl li, #chapterlist li, ul li:has(div.chbox):has(div.eph-num)").Each(func(i int, s *goquery.Selection) {
		link := s.Find("a")
		url, exists := link.Attr("href")
		if !exists {
			return
		}

		chapterNumber := m.extractChapterNumber(url)

		chapters = append(chapters, sources.Chapter{
			Number:    chapterNumber,
			URL:       m.ensureAbsoluteURL(url),
		})
	})

	return chapters, nil
}

func (m *MadaraScans) FetchPages(ctx context.Context, client *httpclient.HTTPClient, chapter sources.Chapter) ([]sources.Page, error) {
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

	return m.parsePages(resp.Body)
}

func (m *MadaraScans) parsePages(body io.ReadCloser) ([]sources.Page, error) {
    searchKeyword := `madarascans\.com\\/wp-content\\/uploads\\/manga\\/[^"]+`   

    var page []sources.Page
    
    re := regexp.MustCompile(searchKeyword)
    strData, err := util.BodyToString(body)
    if err != nil {
        return nil, err
    }
    
    matches := re.FindAllString(strData, -1)
    for i, match := range matches {
        cleanUrl := strings.ReplaceAll(match, "\\", "")
        
        page = append(page, sources.Page{
            Number: i,
            URL: fmt.Sprintf("https://%s", cleanUrl),
        })
    }

    return page, nil
}

func (m *MadaraScans) extractChapterNumber(text string) float32 {
    result := regexp.MustCompile(`chapter-(\d+)`)

    match := result.FindStringSubmatch(text)
    if len(match) > 1 {
        return util.StringToFloat(strings.TrimSpace(match[1]))
    }

	return 0
}

func (m *MadaraScans) ensureAbsoluteURL(url string) string {
	if strings.HasPrefix(url, "http") {
		return url
	}
	if strings.HasPrefix(url, "//") {
		return "https:" + url
	}
	if strings.HasPrefix(url, "/") {
		return m.GetBaseURL() + url
	}

	return m.GetBaseURL() + "/" + url
}
