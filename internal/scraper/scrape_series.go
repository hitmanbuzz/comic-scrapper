package scraper

import (
	"comicrawl/internal/config"
	"comicrawl/internal/cstructs/download_data"
	"comicrawl/internal/cstructs/scrape_data"
	"comicrawl/internal/httpclient"
	"comicrawl/internal/registry"
	"comicrawl/internal/sources"
	"comicrawl/internal/util/fileio"
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// This will scrape the series and save all the url of pages (images) of their respective chapter number
//
// Will save every series from the same source provider in the same json file

func SaveAllSeriesData(ctx context.Context, logger *slog.Logger, client *httpclient.HTTPClient, cfg *config.Config) error {
	logger.Info("processing all sources")

	sourceList := registry.AddSources(logger)

	for _, src := range sourceList {
		logger.Info("processing source", "source name", src.GetName())

		if err := client.ConfigureForDomain(ctx, src.GetBaseURL(), nil, cfg.HTTPProxy); err != nil {
			logger.Warn("failed to configure HTTP client for source domain",
				"source", src.GetName(),
				"domain", src.GetBaseURL(),
				"error", err)
			continue
		}

		jsonFile := fmt.Sprintf("%s/%s/%s_series.json", cfg.LocalDir, cfg.SeriesListDir, src.GetName())
		seriesData, err := fileio.ReadSourceSeries(jsonFile)
		if err != nil {
			return fmt.Errorf("couldn't read source series json file | json file: %s\n", jsonFile)
		}

		var sourceSeriesData download_data.DownloadData
		sourceSeriesData.ScanName = src.GetName()
		sourceSeriesData.ScanURL = src.GetBaseURL()
		var totalSeries, totalImages int64

		var wg sync.WaitGroup
		var mu sync.Mutex

		for _, series := range seriesData.Series {
			// Capture loop variable to avoid closure issues
			sourceSeries := sources.Series{
				URL:    series.ComicPageUrl,
				Title:  series.MainTitle,
				Status: series.ComicStatus,
			}

			wg.Add(1)
			go func(s sources.Series, ser scrape_data.ScanSeriesResponse) {
				defer wg.Done()

				logger.Info("starting series", "series url", s.URL)

				seriesChapter, err := src.FetchChapters(ctx, client, s)
				if err != nil {
					logger.Error("failed to fetch chapter", "series url", s.URL)
					return
				}

				if len(seriesChapter) == 0 {
					logger.Warn("no chapter found", "series url", s.URL)
					return
				}

				logger.Info(
					"found chapter",
					"series url", s.URL,
					"chapter count", len(seriesChapter),
				)

				seriesDownloadData := download_data.SeriesData{
					SeriesID:     ser.MuSeriesId,
					SeriesURL:    ser.ComicPageUrl,
					SeriesName:   ser.MainTitle,
					TotalChapter: len(seriesChapter),
				}

				var pg sync.WaitGroup
				var chapterMu sync.Mutex
				for _, c := range seriesChapter {
					pg.Add(1)

					go func(ch sources.Chapter) {
						defer pg.Done()

						chapterData := download_data.ChapterData{
							ChapterNumber: ch.Number,
							ChapterURL:    ch.URL,
							ChapterName:   ch.Title,
						}

						pages, err := src.FetchPages(ctx, client, ch)
						if err != nil {
							logger.Error(
								"failed to fetch images for chapter",
								"series url", seriesDownloadData.SeriesURL,
								"chapter number", chapterData.ChapterNumber,
							)
							return
						}

						if len(pages) == 0 {
							logger.Warn(
								"no images found for chapter",
								"series url", seriesDownloadData.SeriesURL,
								"chapter number", chapterData.ChapterNumber,
							)
							return
						}

						for _, p := range pages {
							chapterData.Image = append(chapterData.Image, download_data.ImageData{
								ImagerNumber: p.Number,
								ImageURL:     p.URL,
							})
							time.Sleep(500 * time.Millisecond)
						}

						chapterData.TotalImages = len(chapterData.Image)

						chapterMu.Lock()
						seriesDownloadData.Chapter = append(seriesDownloadData.Chapter, chapterData)
						mu.Lock()
						totalImages += int64(chapterData.TotalImages)
						mu.Unlock()
						chapterMu.Unlock()
					}(c)
					time.Sleep(500 * time.Millisecond)
				}

				pg.Wait()
				seriesDownloadData.TotalImages = 0
				for _, ch := range seriesDownloadData.Chapter {
					seriesDownloadData.TotalImages += ch.TotalImages
				}

				mu.Lock()
				sourceSeriesData.Series = append(sourceSeriesData.Series, seriesDownloadData)
				totalSeries++
				mu.Unlock()
				time.Sleep(5 * time.Second)
			}(sourceSeries, series)
		}
		wg.Wait()

		mu.Lock()
		sourceSeriesData.TotalSeries = int(totalSeries)
		sourceSeriesData.TotalImages = int(totalImages)
		mu.Unlock()

		err = fileio.WriteSeriesData(sourceSeriesData, cfg)
		if err != nil {
			return fmt.Errorf("couldn't write series data | source name: %s\n", sourceSeriesData.ScanName)
		}
	}

	return nil
}
