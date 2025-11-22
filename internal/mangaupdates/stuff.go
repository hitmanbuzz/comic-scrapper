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
    if !util.FileExists(jsonFile) {
        fmt.Printf("%s doesn't exist\n", jsonFile)
        return
    }

    response := util.ReadSourceSeriesJson(jsonFile)    

    groupId := response.MuGroupId
    allSeries := GetAllGroupSeries(groupId, client)
    // total := response.TotalSeries
    foundCounter := 0
    counter := 0

    fmt.Println("Starting Filtering...")
    
    for i := range response.Series {
        // fmt.Printf("Checked: %d/%d", counter, total)
        breakStatus := false
        for _, mu := range allSeries {
            // Dumb way but just make it work for now
            if breakStatus {
                break
            }

            ok, _, err := util.IsSimilarEnough(response.Series[i].MainTitle, mu.SeriesData.Title, 0.8)

            if err != nil {
                fmt.Printf("[ERROR] Couldn't do string comparison | S1: %s | S2: %s\n", response.Series[i].MainTitle, mu.SeriesData.Title)
                continue
            }
            
            if ok {
                fmt.Printf("Found: %s\n", mu.SeriesData.Title)
                response.Series[i].Found = true
                response.Series[i].LastUpdated = mu.lastUpdated
                response.Series[i].ComicMuID = mu.SeriesData.SeriesId
                foundCounter++
                break
            }
            for _, t := range mu.SeriesData.AltTitles {
                ok, _, err = util.IsSimilarEnough(response.Series[i].MainTitle, t.Title, 0.8)

                if err != nil {
                    fmt.Printf("[ERROR] Couldn't do string comparison | S1: %s | S2: %s\n", response.Series[i].MainTitle, t.Title)
                    continue
                }

                if ok {
                    fmt.Printf("Found: %s\n", t.Title)
                    response.Series[i].Found = true
                    response.Series[i].LastUpdated = mu.lastUpdated
                    response.Series[i].ComicMuID = mu.SeriesData.SeriesId
                    foundCounter++
                    breakStatus = true
                    break
                }
            }
        }
        counter++
    }

    // I put these printf code here when I was making this in a testing environment
    // Will be remove once we setup a proper loggin system
    // fmt.Printf("Check: %d/%d\n", counter, total)
    // fmt.Printf("Found: %d\n", foundCounter)
    // fmt.Printf("Not Found: %d\n", total - foundCounter)

    fmt.Printf("[%s] Finished Filtering\n", response.GroupName)
        
    util.WriteSourceSeriesJson(response)
}

type AllSeriesData struct {
    SeriesData    cstructs.SeriesResponse
    lastUpdated   int64
}

// This function is just a wrapper in top of `GetSeriesByGroup` function to get all series from the group using their group id
func GetAllGroupSeries(groupId int64, client *httpclient.HTTPClient) []AllSeriesData {
    var allSeries []AllSeriesData
    _, groupSeriesData, err := GetSeriesByGroup(groupId, client)

    if err != nil {
        fmt.Printf("ERROR getting group series: %v\n", err)
        return allSeries
    }

    // fmt.Println("Allocate Size:", len(groupSeriesData.SeriesTitles))
    // fmt.Println("Starting Allocation Series")

    var mu sync.Mutex
    counter := 0
    
    // Batch Size = Number of concurrent process running before putting to sleep timer
    batchSize := 10
    totalSeries := len(groupSeriesData.SeriesTitles)
    
    // God knows what I did here, I was literally blind  and didn't use much brain when I did this (hitman)
    for i := 0; i < totalSeries; i += batchSize {
        end := min(i + batchSize, totalSeries)
        
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
                // fmt.Println("Done:", counter)
                mu.Unlock()
            }(groupSeries)
        }
        
        wg.Wait()
        
        // Sleep between batches (except after the last batch)
        if end < totalSeries {
            fmt.Printf("Process: %d/%d\n", counter, totalSeries)
            fmt.Println("Batch complete. Sleeping before next batch...")
            fmt.Printf("\n")
            // 3 seconds sleep so that it doesn't get ratelimit after every batch of concurrent process
            time.Sleep(3 * time.Second)
        }
    }

    fmt.Println("Finished Getting All Group Series")
    return allSeries
}
