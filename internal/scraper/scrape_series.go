package scraper

import (
	"comicrawl/internal/cstructs"
	"comicrawl/internal/httpclient"
	"comicrawl/internal/registry"
	"context"
	"fmt"
	"log/slog"
)

// This is using the `registry` source provider to generate those <source>_series.json file for each source provider (aka scanlator)
func AddSourcesSeries(client *httpclient.HTTPClient, logger *slog.Logger) []cstructs.FullSeriesResponse {
    var sourcesSeries []cstructs.FullSeriesResponse

    sources := registry.AddSources(logger)

    for _, s := range sources {
        seriesList, err := s.ListSeries(context.TODO(), client)
        fmt.Println("Total Series:", len(seriesList.Series))
        if err != nil {
            fmt.Printf("[ERROR] Source: %s couldn't fetch list of series | [SKIPPING]\n", s.GetName())
            continue
        }

        sourcesSeries = append(sourcesSeries, cstructs.FullSeriesResponse{
            GroupName:   s.GetName(),
            MuGroupId:   s.GetMuGroupID(),
            TotalSeries: len(seriesList.Series),
            Series:      seriesList.Series,
        })
    }

    return sourcesSeries
}
