package registry

import (
	"comicrawl/internal/cloudflare"
	"comicrawl/internal/config"
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


func AddSourcesSeries(ctx context.Context,cfg *config.Config, client *httpclient.HTTPClient, flareClient *cloudflare.Client, logger *slog.Logger) []scrape_data.FullSeriesResponse {
	var sourcesSeries []scrape_data.FullSeriesResponse

	sources := AddSources(logger)

	for _, s := range sources {
		if err := client.ConfigureForDomain(ctx, s.GetBaseURL(), flareClient, cfg.HTTPProxy); err != nil {
			logger.Warn("failed to configure HTTP client for source domain",
				"source", s.GetName(),
				"domain", s.GetBaseURL(),
				"error", err,
			)
		}
		
		seriesList, err := s.ListSeries(ctx, client)
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
