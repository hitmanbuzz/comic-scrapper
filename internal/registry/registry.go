package registry

import (
	"comicrawl/internal/cstructs/scrape_data"
	"comicrawl/internal/httpclient"
	"comicrawl/internal/sources"
	"comicrawl/internal/sources/scanlators"
	"context"
	"log/slog"
)

// AddSources returns all available source implementations
func AddSources(logger *slog.Logger) []sources.Source {
	return []sources.Source{
		scanlators.NewAsuraScans(logger),
		// scanlators.NewWebtoon(logger),
		// scanlators.NewUtoon(logger),
		// scanlators.NewFlameComics(logger),
		// scanlators.NewDrakeScans(logger),
		// scanlators.NewRizzFables(logger),
		// scanlators.NewHiveScans(logger),
		// scanlators.NewThunderScans(logger),
		// scanlators.NewMadaraScans(logger),
	}
}


func AddSourcesSeries(client *httpclient.HTTPClient, logger *slog.Logger) []scrape_data.FullSeriesResponse {
	var sourcesSeries []scrape_data.FullSeriesResponse

	sources := AddSources(logger)

	for _, s := range sources {
		seriesList, err := s.ListSeries(context.TODO(), client)
		logger.Info("fetched series from source", "source", s.GetName(), "count", len(seriesList.Series))
		if err != nil {
			logger.Error("failed to fetch series from source", "source", s.GetName(), "error", err)
			continue
		}

		sourcesSeries = append(sourcesSeries, scrape_data.FullSeriesResponse{
			GroupName:   s.GetName(),
			MuGroupIds:  s.GetMuGroupIDs(),
			TotalSeries: len(seriesList.Series),
			Series:      seriesList.Series,
		})
	}

	return sourcesSeries
}
