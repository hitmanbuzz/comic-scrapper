package scraper

import (
	"comicrawl/internal/cstructs"
	"comicrawl/internal/httpclient"
	"comicrawl/internal/registry"
	"context"
	"log/slog"
)

// This is using the `registry` source provider to generate those <source>_series.json file for each source provider (aka scanlator)
func AddSourcesSeries(client *httpclient.HTTPClient, logger *slog.Logger) []cstructs.FullSeriesResponse {
	var sourcesSeries []cstructs.FullSeriesResponse

	sources := registry.AddSources(logger)

	for _, s := range sources {
		seriesList, err := s.ListSeries(context.TODO(), client)
		logger.Info("fetched series from source", "source", s.GetName(), "count", len(seriesList.Series))
		if err != nil {
			logger.Error("failed to fetch series from source", "source", s.GetName(), "error", err)
			continue
		}

		sourcesSeries = append(sourcesSeries, cstructs.FullSeriesResponse{
			GroupName:   s.GetName(),
			MuGroupIds:  s.GetMuGroupIDs(),
			TotalSeries: len(seriesList.Series),
			Series:      seriesList.Series,
		})
	}

	return sourcesSeries
}
