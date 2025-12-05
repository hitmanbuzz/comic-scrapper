// Provides a client for the MangaUpdates API.
package mangaupdates

import (
	"comicrawl/internal/cstructs"
	"comicrawl/internal/httpclient"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// Return (Total Series, Series List with data, error)
func GetSeriesByGroup(ctx context.Context, groupId int64, client *httpclient.HTTPClient) (int, cstructs.GroupSeriesResponse, error) {
	// https://www.mangaupdates.com/api/v1/groups/{id}/series
	apiUrl := fmt.Sprintf("https://www.mangaupdates.com/api/v1/groups/%d/series", groupId)
	var response cstructs.GroupSeriesResponse

	req, err := http.NewRequestWithContext(ctx, "GET", apiUrl, nil)
	if err != nil {
		return -1, response, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Add("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return -1, response, fmt.Errorf("request failed: %w", err)
	}

	defer resp.Body.Close()

	err = json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		return -1, response, fmt.Errorf("decode failed: %w", err)
	}

	total_series := len(response.SeriesTitles)

	logger := slog.Default()
	logger.Debug("GetSeriesByGroup completed", "group_id", groupId, "total_series", total_series)

	return total_series, response, nil
}

// Return (groupName, Group Website URL)
func GetGroupInfo(ctx context.Context, groupId int64, client *httpclient.HTTPClient) (string, string, error) {
	type Social struct {
		Site string `json:"site"`
	}

	type Response struct {
		Name   string `json:"name"`
		MuUrl  string `json:"url"`
		Social Social `json:"social"`
	}

	// https://www.mangaupdates.com/api/v1/groups/{id}
	apiUrl := fmt.Sprintf("https://www.mangaupdates.com/api/v1/groups/%d", groupId)

	req, err := http.NewRequestWithContext(ctx, "GET", apiUrl, nil)
	if err != nil {
		return "", "", fmt.Errorf("failed to create request for group info: %w", err)
	}

	req.Header.Add("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("request failed for group info: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("unexpected status code %d for group info", resp.StatusCode)
	}

	var group Response
	err = json.NewDecoder(resp.Body).Decode(&group)
	if err != nil {
		return "", "", fmt.Errorf("failed to decode response for group info: %w", err)
	}

	return group.Name, group.Social.Site, nil
}

func GetSeriesInfo(ctx context.Context, seriesId int64, client *httpclient.HTTPClient) (cstructs.SeriesResponse, error) {
	logger := slog.Default()
	var series cstructs.SeriesResponse
	apiUrl := fmt.Sprintf("https://www.mangaupdates.com/api/v1/series/%d", seriesId)

	req, err := http.NewRequestWithContext(ctx, "GET", apiUrl, nil)
	if err != nil {
		logger.Warn("failed to create request for series info", "url", apiUrl, "series_id", seriesId, "error", err)
		return series, err
	}

	req.Header.Add("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		logger.Warn("request failed for series info", "url", apiUrl, "series_id", seriesId, "error", err)
		return series, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return cstructs.SeriesResponse{}, fmt.Errorf("bad status %d for series %d", resp.StatusCode, seriesId)
	}

	err = json.NewDecoder(resp.Body).Decode(&series)
	if err != nil {
		return cstructs.SeriesResponse{}, fmt.Errorf("decode failed for series %d: %w", seriesId, err)
	}

	return series, nil
}

// Will be use to check new series update
func GetSeriesRssFeed(ctx context.Context, seriesId int64, client *httpclient.HTTPClient) (cstructs.RssSeriesData, error) {
	type Item struct {
		Title       string `xml:"title"`
		Description string `xml:"description"`
	}

	type Channel struct {
		Title       string `xml:"title"`
		Link        string `xml:"link"`
		Description string `xml:"description"`
		Items       []Item `xml:"item"`
	}

	type RSS struct {
		XMLName xml.Name `xml:"rss"`
		Version string   `xml:"version,attr"`
		Channel Channel  `xml:"channel"`
	}

	var customData cstructs.RssSeriesData
	apiUrl := fmt.Sprintf("https://www.mangaupdates.com/api/v1/series/%d/rss", seriesId)

	logger := slog.Default()

	req, err := http.NewRequestWithContext(ctx, "GET", apiUrl, nil)
	if err != nil {
		logger.Warn("failed to create request for series RSS feed", "url", apiUrl, "series_id", seriesId, "error", err)
		return customData, err
	}

	req.Header.Add("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		logger.Warn("request failed for series RSS feed", "url", apiUrl, "series_id", seriesId, "error", err)
		return customData, err
	}

	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Warn("couldn't parse response body to bytes for series RSS feed", "series_id", seriesId, "error", err)
		return customData, err
	}

	var rss RSS
	err = xml.Unmarshal(data, &rss)

	if err != nil {
		return customData, err
	}

	customData.Title = rss.Channel.Title

	var chapterData []cstructs.RssSeriesChapter
	for _, item := range rss.Channel.Items {
		chapterData = append(chapterData, cstructs.RssSeriesChapter{
			Chapter:   item.Title,
			Scanlator: item.Description,
		})
	}

	customData.ChapterData = chapterData
	parseRSSData(&customData)

	return customData, nil
}

// This function will properly parse and extract the exact data from the XML RSS Feed
//
// This will be use with `GetSeriesRssFeed` function to properly parse the data
func parseRSSData(data *cstructs.RssSeriesData) {
	// Parse Title
	title := strings.Replace(data.Title, " - Releases on MangaUpdates", "", 1)
	title = strings.TrimSpace(title)
	data.Title = title

	var newChapter []cstructs.RssSeriesChapter

	// Parse Chapter Number
	for _, a := range data.ChapterData {
		chapterData := strings.ReplaceAll(a.Chapter, data.Title, "")
		chapterData = strings.ReplaceAll(chapterData, "c.", "")
		chapterData = strings.TrimSpace(chapterData)

		newChapter = append(newChapter, cstructs.RssSeriesChapter{
			Chapter:   chapterData,
			Scanlator: a.Scanlator,
		})
	}

	data.ChapterData = newChapter
}
