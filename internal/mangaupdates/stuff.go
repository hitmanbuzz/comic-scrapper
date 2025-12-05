package mangaupdates

import (
	"comicrawl/internal/cstructs"
	"comicrawl/internal/httpclient"
	"comicrawl/internal/util"
	"comicrawl/internal/util/fileio"
	"context"
	"fmt"
	"log/slog"
)

// This will filter those comics found in MU (using MuGroupSeries & MuSeriesInfo APIs) from the series.json file
//
// I recommend to use this function inside a sync.Group and insert scanlator json file as the paramater everytime
func FilterScanlatorsFromMu(ctx context.Context, jsonFile string, client *httpclient.HTTPClient) {
	logger := slog.Default()

	if !fileio.PathExists(jsonFile) {
		logger.Error("json file doesn't exist", "file", jsonFile)
		return
	}

	response, err := fileio.ReadSourceSeries(jsonFile)
	if err != nil {
		return
	}

	// Collect series from all group IDs
	var allSeries []AllSeriesData
	for _, groupId := range response.MuGroupIds {
		logger.Info("fetching series for group", "group", response.GroupName, "group_id", groupId)
		groupSeries, err := GetAllGroupSeries(ctx, groupId, client)
		if err != nil {
			logger.Warn("error getting group series, continuing with partial results", "group_id", groupId, "error", err)
		}
		allSeries = append(allSeries, groupSeries...)
	}

	// total := response.TotalSeries
	foundCounter := 0
	counter := 0

	logger.Info("starting filtering", "total_series", response.TotalSeries, "group", response.GroupName)

	for i := range response.Series {
		breakStatus := false
		for _, mu := range allSeries {
			// Dumb way but just make it work for now
			if breakStatus {
				break
			}

			ok, _, matchErr := util.IsComicTitleMatch(response.Series[i].MainTitle, mu.SeriesData.Title)

			if matchErr != nil {
				continue
			}

			if ok {
				response.Series[i].Found = true
				response.Series[i].LastUpdated = mu.lastUpdated
				response.Series[i].MuSeriesId = mu.SeriesData.SeriesId
				foundCounter++
				break
			}
			for _, t := range mu.SeriesData.AltTitles {
				ok, _, err = util.IsComicTitleMatch(response.Series[i].MainTitle, t.Title)

				if err != nil {
					continue
				}

				if ok {
					response.Series[i].Found = true
					response.Series[i].LastUpdated = mu.lastUpdated
					response.Series[i].MuSeriesId = mu.SeriesData.SeriesId
					foundCounter++
					breakStatus = true
					break
				}
			}
		}
		counter++
	}

	response.FoundSeries = foundCounter

	logger.Info("finished filtering", "group", response.GroupName, "found_series", foundCounter, "total_series", response.TotalSeries)
	err = fileio.WriteSourceSeries(response)
	if err != nil {
		logger.Error("couldn't write filter data for source series json", "group", response.GroupName, "error", err)
		return
	}
}

type AllSeriesData struct {
	SeriesData  cstructs.SeriesResponse
	lastUpdated int64
}

// This function is just a wrapper in top of `GetSeriesByGroup` function to get all series from the group using their group id
func GetAllGroupSeries(ctx context.Context, groupId int64, client *httpclient.HTTPClient, opts ...BatchOption) ([]AllSeriesData, error) {
	logger := slog.Default()
	_, groupSeriesData, err := GetSeriesByGroup(ctx, groupId, client)

	if err != nil {
		return nil, fmt.Errorf("error getting group series: %w", err)
	}

	logger.Info("starting to process group series", "group_id", groupId, "total_series", len(groupSeriesData.SeriesTitles))
	
	allSeries, err := ProcessSeriesTitles(ctx, client, groupSeriesData.SeriesTitles, opts...)
	if err != nil {
		return allSeries, fmt.Errorf("error processing group series: %w", err)
	}

	logger.Info("finished getting all group series", "total_series", len(allSeries), "group_id", groupId)
	return allSeries, nil
}
