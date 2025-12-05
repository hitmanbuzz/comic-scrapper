package scraper

// From Hitman
//
// I really wish this whole part to be refactor
// This really is a trigger point for any new person joining
//
// God knows WTF is happening, it is connected to every single part of the codebase
//
// That `runScraper` function is ~300 lines of code.
// I would recommend every function to be less 80-100 lines of code

import (
	"comicrawl/internal/aria2c"
	"comicrawl/internal/cloudflare"
	"comicrawl/internal/config"
	"comicrawl/internal/disk"
	"comicrawl/internal/httpclient"
	"comicrawl/internal/registry"
	"comicrawl/internal/sources"
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

type ScrapeMode string

const (
	ModeFull        ScrapeMode = "full"
	ModeIncremental ScrapeMode = "incremental"
	ModeSingle      ScrapeMode = "single"
)

// Define an interface for sources that can compare chapters
type ChapterComparator interface {
	CompareChapters(localChapters []disk.Chapter, remoteChapters []sources.Chapter) (newChapters []sources.Chapter, updatedChapters []sources.Chapter)
}

// Downloader interface for streaming downloads
type Downloader interface {
	AddDownload(request aria2c.DownloadRequest)
	Close() error
}

func RunScraper(
	ctx context.Context,
	cfg *config.Config,
	storageClient *disk.Client,
	flareClient *cloudflare.Client,
	httpClient *httpclient.HTTPClient,
	downloader Downloader,
	logger *slog.Logger,
	mode ScrapeMode,
) error {
	startTime := time.Now()
	var totalChapters, totalPages int64

	logger.Info("starting streaming scraper",
		"bucket", cfg.Bucket,
		"mode", mode)

	// Collect metadata updates for batch processing at the end
	type metadataUpdate struct {
		seriesSlug string
		metadata   *disk.SeriesMetadata
	}

	var pendingUpdates []metadataUpdate
	var updatesMutex sync.Mutex

	sourceList := registry.AddSources(logger)

	// Filter sources based on configuration
	if cfg.HasSourceFilters() {
		var filtered []sources.Source
		for _, source := range sourceList {
			if cfg.IsSourceIncluded(source.GetName()) {
				filtered = append(filtered, source)
			}
		}
		sourceList = filtered
	}

	var wg sync.WaitGroup

	// Process all sources and series concurrently
	for _, src := range sourceList {
		logger.Info("processing source", "source", src.GetName())

		// Configure HTTP client for this source's domain
		if err := httpClient.ConfigureForDomain(ctx, src.GetBaseURL(), flareClient, cfg.HTTPProxy); err != nil {
			logger.Warn("failed to configure HTTP client for source domain",
				"source", src.GetName(),
				"domain", src.GetBaseURL(),
				"error", err)
			continue
		}

		// Fetch series from source
		seriesData, err := src.ListSeries(ctx, httpClient.Client())
		if err != nil {
			logger.Error("failed to fetch series from source",
				"source", src.GetName(),
				"error", err)
			continue
		}

		logger.Info("fetched series from source",
			"source", src.GetName(),
			"count", len(seriesData.Series))

		// Log first few series for debugging
		if len(seriesData.Series) > 0 {
			logger.Debug("sample series slugs",
				"first_5", seriesData.Series[:min(5, len(seriesData.Series))])
		}

		// Process each series
		seriesCount := 0
		for _, series := range seriesData.Series {
			// NOTE: I don't think we need this anymore
			// Probably we can make something that can scrape specific titles only but for now not needed
			// If really needed just change all `found_mu` to false and only change the one you need to true

			// Check if we should process this series
			// if !cfg.IsSeriesIncluded(series.Slug) {
			// 	logger.Debug("skipping series", "series", series.Slug)
			// 	continue
			// }

			// Check series limit
			if cfg.LimitSeries > 0 && seriesCount >= cfg.LimitSeries {
				logger.Info("series limit reached", "limit", cfg.LimitSeries)
				break
			}

			seriesCount++

			// Convert cstructs.ScanSeriesResponse to sources.Series
			sourceSeries := sources.Series{
				URL:    series.ComicPageUrl,
				Title:  series.MainTitle,
				Status: series.ComicStatus,
			}

			wg.Add(1)
			go func(s sources.Series) {
				defer wg.Done()

				logger.Info("processing series",
					"source", src.GetName(),
					"series", s.URL,
					"title", s.Title)

				// Load existing metadata
				localMeta, err := storageClient.LoadSeriesMetadata(ctx, s.URL)
				if err != nil {
					logger.Warn("failed to load series metadata from storage",
						"series", s.URL,
						"error", err)
					localMeta = &disk.SeriesMetadata{}
				}

				// Handle different scraping modes
				if mode == ModeIncremental || mode == ModeSingle {
					// For incremental mode, skip series that don't exist locally
					if mode == ModeIncremental && len(localMeta.Chapters) == 0 {
						logger.Debug("skipping new series in incremental mode", "series", s.URL)
						return
					}

					// For single mode, only process explicitly included series
					if mode == ModeSingle && !cfg.IsSeriesIncluded(s.URL) {
						logger.Debug("skipping series not in include list for single mode", "series", s.URL)
						return
					}
				}

				// Fetch chapters from source
				remoteChapters, err := src.FetchChapters(ctx, httpClient.Client(), s)
				if err != nil {
					logger.Error("failed to fetch chapters from source",
						"series", s.URL,
						"error", err)
					return
				}

				if len(remoteChapters) == 0 {
					logger.Info("no chapters found", "series", s.URL)
					return
				}

				// Apply chapter limit if configured
				if cfg.LimitChapters > 0 && len(remoteChapters) > cfg.LimitChapters {
					logger.Info("limiting chapters", "series", s.URL, "original", len(remoteChapters), "limited", cfg.LimitChapters)
					remoteChapters = remoteChapters[:cfg.LimitChapters]
				}

				// Filter chapters based on mode
				var chaptersToProcess []sources.Chapter
				if mode == ModeIncremental && len(localMeta.Chapters) > 0 {
					// In incremental mode, only process new chapters
					// Try to use CompareChapters if available
					if comparator, ok := src.(ChapterComparator); ok {
						newChapters, updatedChapters := comparator.CompareChapters(localMeta.Chapters, remoteChapters)
						chaptersToProcess = newChapters
						// Note: updatedChapters could be used for other purposes if needed
						if len(updatedChapters) > 0 {
							logger.Debug("found updated chapters", "count", len(updatedChapters), "series", s.URL)
						}
					} else {
						// Fallback: process all chapters
						logger.Debug("source doesn't implement ChapterComparator, processing all chapters", "source", src.GetName())
						chaptersToProcess = remoteChapters
					}
					logger.Info("filtering chapters in incremental mode",
						"series", s.URL,
						"total_remote", len(remoteChapters),
						"new_chapters", len(chaptersToProcess))
				} else {
					// In full mode or for new series, process all chapters
					chaptersToProcess = remoteChapters
				}

				if len(chaptersToProcess) == 0 {
					logger.Info("no new chapters to process",
						"series", s.URL,
						"mode", mode)
					return
				}

				logger.Info("found chapters to process",
					"series", s.URL,
					"chapters", len(chaptersToProcess),
					"mode", mode)

				// Track totals for chapters that will actually be processed
				atomic.AddInt64(&totalChapters, int64(len(chaptersToProcess)))
				for _, ch := range chaptersToProcess {
					atomic.AddInt64(&totalPages, int64(len(ch.Pages)))
				}

				// Process chapters and stream downloads immediately (unless in dry-run mode)
				if !cfg.DryRun {
					err = processSeriesChapters(ctx, src, httpClient, s, chaptersToProcess, downloader, storageClient, logger)
					if err != nil {
						logger.Error("failed to process chapters",
							"series", s.URL,
							"error", err)
						return
					}
				} else {
					logger.Info("dry-run mode: skipping chapter processing", "series", s.URL, "chapters", len(chaptersToProcess))
				}

				// Prepare metadata update
				localMeta.Title = s.Title
				localMeta.Status = s.Status
				localMeta.UpdatedAt = time.Now()

				// Convert remote chapters to disk storage format (include all chapters)
				oldChapters := localMeta.Chapters
				localMeta.Chapters = make([]disk.Chapter, len(remoteChapters))
				for i, chap := range remoteChapters {
					// Preserve existing upload time if chapter exists
					var uploadedAt time.Time
					for _, existingChap := range oldChapters {
						if existingChap.Number == chap.Number {
							uploadedAt = existingChap.UploadedAt
							break
						}
					}

					localMeta.Chapters[i] = disk.Chapter{
						Number:     chap.Number,
						Title:      chap.Title,
						Pages:      len(chap.Pages),
						UploadedAt: uploadedAt,
						SourceURL:  chap.URL,
					}
				}

				// Add to pending updates
				updatesMutex.Lock()
				pendingUpdates = append(pendingUpdates, metadataUpdate{
					seriesSlug: s.URL,
					metadata:   localMeta,
				})
				updatesMutex.Unlock()
			}(sourceSeries)
		}
	}

	// Wait for all series to be processed (downloads are streaming concurrently)
	logger.Info("waiting for all series processing to complete")
	wg.Wait()

	// Update metadata (unless in dry-run mode)
	logger.Info("updating metadata", "updates_count", len(pendingUpdates))

	var metadataErrors int64
	if cfg.DryRun {
		logger.Info("dry-run mode: skipping metadata updates")
	} else {
		var metadataWg sync.WaitGroup

		for _, update := range pendingUpdates {
			metadataWg.Add(1)
			go func(u metadataUpdate) {
				defer metadataWg.Done()
				if err := storageClient.SaveSeriesMetadata(ctx, u.seriesSlug, u.metadata); err != nil {
					atomic.AddInt64(&metadataErrors, 1)
					logger.Error("failed to save series metadata",
						"series", u.seriesSlug,
						"error", err)
				} else {
					logger.Debug("metadata updated",
						"series", u.seriesSlug)
				}
			}(update)
		}

		metadataWg.Wait()

		if metadataErrors > 0 {
			logger.Warn("metadata update errors", "count", metadataErrors)
		}
	}

	// Performance summary
	duration := time.Since(startTime)
	logger.Info("performance summary",
		"total_time_sec", duration.Seconds(),
		"total_chapters", atomic.LoadInt64(&totalChapters),
		"total_pages", atomic.LoadInt64(&totalPages),
		"chapters_per_sec", float64(atomic.LoadInt64(&totalChapters))/duration.Seconds(),
		"pages_per_sec", float64(atomic.LoadInt64(&totalPages))/duration.Seconds())

	logger.Info("scraper completed successfully",
		"metadata_updates", len(pendingUpdates)-int(metadataErrors),
		"metadata_errors", metadataErrors)

	return nil
}

// processSeriesChapters processes chapters and streams downloads immediately
func processSeriesChapters(ctx context.Context, src sources.Source, httpClient *httpclient.HTTPClient,
	series sources.Series, remoteChapters []sources.Chapter, downloader Downloader, storageClient *disk.Client, logger *slog.Logger) error {
	var wg sync.WaitGroup
	var processedCount int64
	var errorCount int64

	// Process chapters concurrently
	for _, chapter := range remoteChapters {
		wg.Add(1)
		go func(ch sources.Chapter) {
			defer wg.Done()

			logger.Debug("fetching pages for chapter",
				"series", series.URL,
				"chapter", ch.Number)

			// Fetch page URLs for this chapter
			pages, err := src.FetchPages(ctx, httpClient.Client(), ch)
			if err != nil {
				atomic.AddInt64(&errorCount, 1)
				logger.Error("failed to fetch pages for chapter",
					"series", series.URL,
					"chapter", ch.Number,
					"error", err)
				return
			}

			if len(pages) == 0 {
				logger.Warn("no pages found for chapter",
					"series", series.URL,
					"chapter", ch.Number)
				return
			}

			// Create download requests and stream immediately
			diskChapter := disk.Chapter{
				Number:    ch.Number,
				Title:     ch.Title,
				Pages:     len(pages),
				SourceURL: ch.URL,
			}

			// Stream each page download immediately
			for _, page := range pages {
				downloader.AddDownload(aria2c.DownloadRequest{
					SeriesSlug:    series.URL,
					Chapter:       diskChapter,
					Page:          page,
					StorageClient: storageClient,
				})
			}

			atomic.AddInt64(&processedCount, 1)
			logger.Info("chapter pages queued for download",
				"series", series.URL,
				"chapter", ch.Number,
				"pages", len(pages),
				"processed", atomic.LoadInt64(&processedCount),
				"total", len(remoteChapters))
		}(chapter)
	}

	wg.Wait()

	finalProcessed := atomic.LoadInt64(&processedCount)
	finalErrors := atomic.LoadInt64(&errorCount)

	logger.Info("chapter processing completed",
		"series", series.URL,
		"processed", finalProcessed,
		"errors", finalErrors,
		"total", len(remoteChapters))

	if finalErrors > 0 {
		return fmt.Errorf("failed to process %d/%d chapters", finalErrors, len(remoteChapters))
	}

	return nil
}
