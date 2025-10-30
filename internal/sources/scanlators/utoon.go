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
	u.Logger.Info("fetching chapters", "series", series.Slug)

	url := fmt.Sprintf("%s/series/%s", u.GetBaseURL(), series.Slug)

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

	return u.parseChaptersPage(doc, series.Slug)

}
func (u *Utoon) FetchPages(ctx context.Context, client *httpclient.HTTPClient, chapter sources.Chapter) ([]sources.Page, error) {
	u.Logger.Info("fetching pages", "chapter", chapter.Number)

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

	return u.parsePages(doc)
}

func (u *Utoon) parsePages(doc *goquery.Document) ([]sources.Page, error) {
	var pages []sources.Page

	doc.Find("div.page-break.no-gaps").Each(func(i int, s *goquery.Selection) {
        // Find the img tag within the div
        img := s.Find("img")
        
        // Get the id attribute (image id has the image number)
        imageID, exists := img.Attr("id")
        if exists {
	        // Get the src attribute (which is the chapter image url)
	        imageSrc, exists := img.Attr("src")
	        if exists {
		        imageNumStr := u.extractImageNumber(imageID)

		        imageUrl := strings.TrimSpace(imageSrc)
				imageNum, err := strconv.Atoi(imageNumStr)

				if err != nil {
					fmt.Printf("Failed to convert `%s` to int\n", imageNumStr)
					return
				}

				pages = append(pages, sources.Page{
					Number: imageNum,
					URL: imageUrl,
				})
	        }
	        // string version of image number
		}        
    })

	u.Logger.Info("parsed pages", "count", len(pages))
	return pages, nil
}

func (u *Utoon) parseSeriesPage(doc *goquery.Document) []sources.Series {
	var series []sources.Series

	doc.Find("h3.h5 a").Each(func(i int, s *goquery.Selection) {
	    href, exists := s.Attr("href")
		if !exists {
			return
		}

		slug, title := u.extractSlugTitleFromUrl(href)

		u.Logger.Info("found series", "title", title, "slug", slug, "url", href)

		if title != "" && slug != "" {
			series = append(series, sources.Series{
				Slug: slug,
				Title: "",
			})
		}		
	})

	return series
}

func (u *Utoon) parseChaptersPage(doc *goquery.Document, seriesSlug string) ([]sources.Chapter, error) {
	var chapters []sources.Chapter

	doc.Find("li.wp-manga-chapter a").Each(func(index int, element *goquery.Selection) {
        chapterUrl, exists := element.Attr("href")
        if exists {
	        if chapterUrl != "#" {
		 		chapters = append(chapters, sources.Chapter{
		 			Number: u.extractChapterNumber(chapterUrl),
		 			Title: "",
					URL: chapterUrl,
					SourceURL: chapterUrl,
		 		})	       
	        }
        }
    })

	u.Logger.Info("parsed chapters", "series", seriesSlug, "count", len(chapters))
	return chapters, nil
}

// This will get the last page for the entire utoon manga (comic) section
//
// URL: https://utoon.net/manga/page/<page-number>/
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

// Extract chapter number from chapter url
func (u *Utoon) extractChapterNumber(chapterUrl string) string {
	// example: https://utoon.net/manga/the-return-of-a-crazy-genius-composer/chapter-76/
	// lastDash = `chapter-` before the chapter number (76)
	// The reason is that we have url like this https://utoon.net/manga/the-return-of-a-crazy-genius-composer/chapter-0-5/
	// `chapter-0-5` = chapter 0.5
	
	// lastSlash = `/` after the chapter number (76)
	// Between these two is the chapter number
	
	fmt.Println("Chapter URL:", chapterUrl)
	lastSlash := strings.LastIndex(chapterUrl, "/")
	lastDash := strings.LastIndex(chapterUrl, "chapter-")

	fmt.Println("Last Dash:", lastDash)
	fmt.Println("Last Slash", lastSlash)
	// `chapter-` is 7 in length so we will start from index 8 which is the chapter number starting position
	chapter_num := chapterUrl[lastDash+8:lastSlash]

	fmt.Println("Chapter Num:", chapter_num)
	if strings.Contains(chapter_num, "-") {
		strings.Replace(chapter_num, "-", ".", 1)
	}

	return chapter_num
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

// Extract Image Number from the imageId
func (u *Utoon) extractImageNumber(imageId string) string {
	fmt.Println("Image ID:", imageId)
	// example: image-2
	dashIndex := strings.Index(imageId, "-")
	return imageId[dashIndex+1:]
}
