package scanlators

import (
	"comicrawl/internal/httpclient"
	"comicrawl/internal/sources"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

type Utoon struct {
	*sources.BaseSource
}

func NewUtoon(logger *slog.Logger) *Utoon {
	return &Utoon{
		BaseSource: sources.NewBaseSource("utoon", "https://utoon.net", logger),
	}
}

func (u *Utoon) GetChaptersUrl() {
	// url := fmt.Sprintf("%s/manga/%s", u.baseURL, "")
}

func (u *Utoon) ListSeries(ctx context.Context, client *httpclient.HTTPClient) ([]sources.Series, error) {
	u.Logger.Info("fetching series list from Utoon")

	var allSeries []sources.Series
	page := 1
	last_page := 0

	for {
		if page > 1 && page + 1 > last_page {
			break
		}
		
		page_url := fmt.Sprintf("%s/manga/page/%d/", u.BaseURL, page)
		u.Logger.Debug("fetching series page", "page", page, "url", page_url)

		req, err := http.NewRequestWithContext(ctx, "GET", page_url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch series page %d: %w", page, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("unexpected status code; %d", resp.StatusCode)
		}

		doc, err := goquery.NewDocumentFromReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to parse HTML: %w", err)
		}

		if page == 1 {
			last_page = u.getLastPage(doc)
		}

		pageSeries := u.parseSeriesPage(doc)
		// fmt.Println("Page Series:", pageSeries)
		allSeries = append(allSeries, pageSeries...)
		
		// fmt.Printf("Page %d done\n", page)
		page++
	}

	u.Logger.Info("fetched series from Utoon", "count", len(allSeries))
	
	return allSeries, nil
}

func (u *Utoon) FetchChapters(ctx context.Context, client *httpclient.HTTPClient, series sources.Series) ([]sources.Chapter, error) {
	return nil, nil
}
func (u *Utoon) FetchPages(ctx context.Context, client *httpclient.HTTPClient, chapter sources.Chapter) ([]sources.Page, error) {
	return nil, nil
}

func (u *Utoon) parseSeriesPage(doc *goquery.Document) []sources.Series {
	var series []sources.Series

	doc.Find("h3.h5 a").Each(func(i int, s *goquery.Selection) {
	    href, exists := s.Attr("href")
		if !exists {
			return
		}

		slug, title := u.extractSlugTitleFromUrl(href)

		if title != "" && slug != "" {
			series = append(series, sources.Series{
				Slug: slug,
				Title: "",
				Description: "",         
				Author:      "",         
				Status:      "",         
				Genres:      []string{}, 
				Chapters:    []sources.Chapter{},
			})
		}		
	})

	return series
}

func (u *Utoon) parseChaptersPage(doc *goquery.Document, seriesSlug string) ([]sources.Chapter, error) {
	var chapters []sources.Chapter

	doc.Find("li.wp-manga-chapter a").Each(func(index int, element *goquery.Selection) {
        // Extract href attribute
        href, exists := element.Attr("href")
        if exists {
            fmt.Printf("Link %d: %s\n", index, href)
        }
    })

	return chapters, nil
}

func (u *Utoon) getLastPage(doc *goquery.Document) int {
	last_page_url := ""

	href, exists := doc.Find("a.last").First().Attr("href")
	if exists {
		last_page_url = href
	}

	last_page_num := 0

	if len(last_page_url) > 0 {
		last_slash_index := strings.LastIndex(last_page_url, "/")
		page_str := last_page_url[29:last_slash_index]

		page_int, err := strconv.Atoi(page_str)

		if err != nil {
			u.Logger.Warn("failed to parse to page number to `int` type", "error:", err)
		}

		last_page_num = page_int
	}

	return last_page_num
}

// Remove Symbols and Replace with Spaces
//
// Then capitalize each word in the string
func (u *Utoon) removeSymbolsCapitalize(s string) string {
	new_str := strings.ReplaceAll(s, "-", " ")

	caser := cases.Title(language.Und)
	result := caser.String(new_str)

	return result
}

// Return (slug, title)
func (u *Utoon) extractSlugTitleFromUrl(url string) (string, string) {
	// https://utoon.net/manga/
	// 24 = index starting after `/`
	//
	// last slash means the last `/` at the end of the url
	//
	// example: https://utoon.net/manga/god-of-martial-arts/

	last_slash := strings.LastIndex(url, "/")
	slug := url[24:last_slash]
	title := u.removeSymbolsCapitalize(slug)

	return slug, title
}
