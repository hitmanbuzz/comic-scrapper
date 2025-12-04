package mangaupdates

import (
	"comicrawl/internal/cstructs"
	"comicrawl/internal/httpclient"
	"comicrawl/internal/util"
	"fmt"
	"sync"
	"time"
)

// This will filter those comics found in MU (using MuGroupSeries & MuSeriesInfo APIs) from the series.json file
//
// I recommend to use this function inside a sync.Group and insert scanlator json file as the paramater everytime
func FilterScanlatorsFromMu(jsonFile string, client *httpclient.HTTPClient) {
	if !util.IsPathExists(jsonFile) {
		fmt.Printf("%s doesn't exist\n", jsonFile)
		return
	}

	response, err := util.ReadSourceSeriesJson(jsonFile)
	if err != nil {
		return
	}

	// Collect series from all group IDs
	var allSeries []AllSeriesData
	for _, groupId := range response.MuGroupIds {
		fmt.Printf("Fetching series for group: %s\n", response.GroupName)
		groupSeries := GetAllGroupSeries(groupId, client)
		allSeries = append(allSeries, groupSeries...)
	}

	// total := response.TotalSeries
	foundCounter := 0
	counter := 0

	fmt.Println("Starting Filtering...")

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

	fmt.Printf("[%s] Finished Filtering\n", response.GroupName)
	err = util.WriteSourceSeriesJson(response)
	if err != nil {
		fmt.Printf("[ERROR] Couldnt' write filter data for source series json | Group: %s\n", response.GroupName)
		return
	}
}

type AllSeriesData struct {
	SeriesData  cstructs.SeriesResponse
	lastUpdated int64
}

// This function is just a wrapper in top of `GetSeriesByGroup` function to get all series from the group using their group id
func GetAllGroupSeries(groupId int64, client *httpclient.HTTPClient) []AllSeriesData {
	var allSeries []AllSeriesData
	_, groupSeriesData, err := GetSeriesByGroup(groupId, client)

	if err != nil {
		fmt.Printf("ERROR getting group series: %v\n", err)
		return allSeries
	}

	var mu sync.Mutex
	counter := 0

	// Batch Size = Number of concurrent process running before putting to sleep timer
	batchSize := 10
	totalSeries := len(groupSeriesData.SeriesTitles)

	// God knows what I did here, I was literally blind  and didn't use much brain when I did this (hitman)
	for i := 0; i < totalSeries; i += batchSize {
		end := min(i+batchSize, totalSeries)

		batch := groupSeriesData.SeriesTitles[i:end]
		var wg sync.WaitGroup

		fmt.Printf("Processing batch: %d to %d\n", i, end)

		for _, groupSeries := range batch {
			wg.Add(1)

			go func(ss cstructs.TitlesStruct) {
				defer wg.Done()

				lastUpdated := ss.LastUpdated.TimeStamp
				series, err := GetSeriesInfo(ss.SeriesId, client)

				if err != nil {
					fmt.Printf("[WARNING]: Skipping series %d: %v\n", ss.SeriesId, err)
					// 1 second sleep after getting ratelimit or any error from the API
					time.Sleep(1000 * time.Millisecond)
					return
				}

				mu.Lock()
				allSeries = append(allSeries, AllSeriesData{
					SeriesData:  series,
					lastUpdated: lastUpdated,
				})
				counter++
				mu.Unlock()
			}(groupSeries)
		}

		wg.Wait()

		// Sleep between batches (except after the last batch)
		if end < totalSeries {
			fmt.Printf("Process: %d/%d\n", counter, totalSeries)
			fmt.Println("Batch complete. Sleeping before next batch...")
			fmt.Printf("\n")
			time.Sleep(100 * time.Millisecond)
		}
	}

	fmt.Println("Finished Getting All Group Series")
	return allSeries
}
