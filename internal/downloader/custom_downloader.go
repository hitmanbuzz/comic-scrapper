package downloader

import (
	"comicrawl/internal/cloudflare"
	"comicrawl/internal/config"
	"comicrawl/internal/cstructs/download_data"
	"comicrawl/internal/httpclient"
	"comicrawl/internal/util"
	"comicrawl/internal/util/fileio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

var sem = make(chan struct{}, 50)

func RunDownload(
	ctx context.Context,
	logger *slog.Logger,
	client *httpclient.HTTPClient,
	cfg *config.Config,
	flareClient *cloudflare.Client,
) error {
	pattern := regexp.MustCompile(`^.+_series_data\.json$`)
	var matchingFiles []string

	seriesDataDir := fmt.Sprintf("%s/%s", cfg.LocalDir, cfg.SeriesDataDir)

	entries, err := os.ReadDir(seriesDataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading directory: %v\n", err)
		os.Exit(1)
	}

	for _, entry := range entries {
		if !entry.IsDir() && pattern.MatchString(entry.Name()) {
			filePath := filepath.Join(seriesDataDir, entry.Name())
			matchingFiles = append(matchingFiles, filePath)
		}
	}

	if len(matchingFiles) == 0 {
		logger.Error("no files matching pattern '*_series_data.json' found", "directory", seriesDataDir)
		os.Exit(1)
	}

	for _, jsonFile := range matchingFiles {
		data, err := fileio.ReadSeriesData(jsonFile)
		if err != nil {
			logger.Error("failed to read series data json file", "json file", jsonFile)
			continue
		}

		var wg sync.WaitGroup
		if err := client.ConfigureForDomain(ctx, data.ScanURL, flareClient, cfg.HTTPProxy); err != nil {
			logger.Warn("failed to configure HTTP client for source domain",
				"source", data.ScanName,
				"domain", data.ScanURL,
				"error", err,
			)
			continue
		}
		for _, series := range data.Series {
			wg.Add(1)
			go func(s download_data.SeriesData) {
				defer wg.Done()
				var cg sync.WaitGroup
				for _, chapter := range series.Chapter {
					cg.Add(1)
					sem <- struct{}{}
					go func(c download_data.ChapterData) {
						defer func() { <-sem }()

						for _, image := range chapter.Image {
							dirPath := fmt.Sprintf(
								"%s/%d/%s/chap_%s",
								cfg.Bucket,
								series.SeriesID,
								data.ScanName,
								util.ChapterFloatToString(float64(c.ChapterNumber)),
							)
							imageFile := fmt.Sprintf("img_%d%s", image.ImagerNumber, filepath.Ext(image.ImageURL))

							err := fileio.DownloadImage(ctx, logger, client, image.ImageURL, dirPath, imageFile)
							if err != nil {
								logger.Error("failed to download image", "url", image.ImageURL, "error", err)
								return
							}
						}
					}(chapter)
				}

				cg.Wait()
			}(series)
		}

		wg.Wait()
	}

	return nil
}

