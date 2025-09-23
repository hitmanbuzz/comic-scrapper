package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"comicrawl/internal/aria2c"
	"comicrawl/internal/config"
	"comicrawl/internal/disk"
	"comicrawl/internal/flaresolverr"
	"comicrawl/internal/httpclient"
	"comicrawl/internal/sources"
	"comicrawl/internal/worker"
)

// Downloader interface for streaming downloads
type Downloader interface {
	AddDownload(request aria2c.DownloadRequest)
	Close() error
}

func main() {
	// Parse command line arguments
	configPath := flag.String("config", "config.yaml", "Path to config file")
	flag.Parse()

	// Load configuration
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Setup logging
	logger := setupLogger(cfg.LogLevel)

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle graceful shutdown
	setupSignalHandler(cancel, logger)

	logger.Info("starting manga scraper", "config", *configPath)

	// Initialize storage client
	var storageClient *disk.Client
	switch cfg.StorageType {
	case "disk":
		storageClient, err = disk.NewClient(ctx, cfg, logger)
		if err != nil {
			logger.Error("failed to create disk storage client", "error", err)
			os.Exit(1)
		}
	default:
		logger.Error("unsupported storage type", "storage_type", cfg.StorageType)
		os.Exit(1)
	}

	// Only create FlareSolverr client if configured
	var flareClient *flaresolverr.Client
	if cfg.FlareSolverrURL != "" {
		flareClient = flaresolverr.NewClient(cfg, logger)
		logger.Info("FlareSolverr client initialized", "url", cfg.FlareSolverrURL)
	} else {
		logger.Info("FlareSolverr disabled - proceeding without Cloudflare bypass")
	}
	
	httpClient, err := httpclient.NewHTTPClient(cfg, logger, flareClient)
	if err != nil {
		logger.Error("failed to create HTTP client", "error", err)
		os.Exit(1)
	}

	// Create downloader based on configuration
	var downloader Downloader
	
	if cfg.UseAria2c {
		logger.Info("using aria2c for streaming downloads", "aria2c_url", cfg.Aria2cURL)
		aria2cDownloader, err := aria2c.NewDownloader(cfg.Aria2cURL, logger)
		if err != nil {
			logger.Error("failed to create aria2c downloader, falling back to regular pool", "error", err)
			workerPool := worker.NewPool(cfg.DownloadWorkers, logger)
			workerPool.Start()
			downloader = workerPool
			defer workerPool.Close()
		} else {
			downloader = aria2cDownloader
			defer aria2cDownloader.Close()
		}
	} else {
		logger.Info("using regular worker pool for downloads")
		workerPool := worker.NewPool(cfg.DownloadWorkers, logger)
		workerPool.Start()
			downloader = workerPool
			defer workerPool.Close()
	}

	// Run the scraper
	if err := runScraper(ctx, cfg, storageClient, flareClient, httpClient, downloader, logger); err != nil {
		logger.Error("scraper failed", "error", err)
		os.Exit(1)
	}

	logger.Info("scraper completed successfully")
}

func runScraper(ctx context.Context, cfg *config.Config, storageClient *disk.Client, flareClient *flaresolverr.Client, httpClient *httpclient.HTTPClient, downloader Downloader, logger *slog.Logger) error {
	startTime := time.Now()
	var totalChapters, totalPages int64

	logger.Info("starting streaming scraper",
		"bucket", cfg.Bucket,
		"use_aria2c", cfg.UseAria2c)

	// Collect metadata updates for batch processing at the end
	type metadataUpdate struct {
		seriesSlug string
		metadata   *disk.SeriesMetadata
	}

	var pendingUpdates []metadataUpdate
	var updatesMutex sync.Mutex

	sourceList := []sources.Source{
		sources.NewAsuraScans(logger),
	}

	var wg sync.WaitGroup
	
	// Process all sources and series concurrently
	for _, src := range sourceList {
		logger.Info("processing source", "source", src.Name())

		// Configure HTTP client for this source's domain
		if err := httpClient.ConfigureForDomain(ctx, src.BaseURL(), flareClient, cfg.HTTPProxy); err != nil {
			logger.Warn("failed to configure HTTP client for source domain",
				"source", src.Name(),
				"domain", src.BaseURL(),
				"error", err)
			continue
		}

		// Fetch series from source
		seriesList, err := src.ListSeries(ctx, httpClient.Client())
		if err != nil {
			logger.Error("failed to fetch series from source",
				"source", src.Name(),
				"error", err)
			continue
		}

		logger.Info("fetched series from source",
			"source", src.Name(),
			"count", len(seriesList))

		// Log first few series for debugging
		if len(seriesList) > 0 {
			logger.Debug("sample series slugs", 
				"first_5", seriesList[:min(5, len(seriesList))])
		}

		// Process each series
		for _, series := range seriesList {
			if !shouldProcessSeries(series.Slug, cfg.ScrapeOnly) {
				logger.Debug("skipping series", "series", series.Slug, "scrape_only", cfg.ScrapeOnly)
				continue
			}

			wg.Add(1)
			go func(s sources.Series) {
				defer wg.Done()

				logger.Info("processing series",
					"source", src.Name(),
					"series", s.Slug,
					"title", s.Title)

				// Load existing metadata
				localMeta, err := storageClient.LoadSeriesMetadata(ctx, s.Slug)
				if err != nil {
					logger.Warn("failed to load series metadata from storage",
						"series", s.Slug,
						"error", err)
					localMeta = &disk.SeriesMetadata{}
				}

				// Fetch chapters from source
				remoteChapters, err := src.FetchChapters(ctx, httpClient.Client(), s)
				if err != nil {
					logger.Error("failed to fetch chapters from source",
						"series", s.Slug,
						"error", err)
					return
				}

				if len(remoteChapters) == 0 {
					logger.Info("no chapters found", "series", s.Slug)
					return
				}

				logger.Info("found chapters to process",
					"series", s.Slug,
					"chapters", len(remoteChapters))

				// Track totals
				atomic.AddInt64(&totalChapters, int64(len(remoteChapters)))
				for _, ch := range remoteChapters {
					atomic.AddInt64(&totalPages, int64(len(ch.Pages)))
				}

				// Process chapters and stream downloads immediately
				err = processSeriesChapters(ctx, src, httpClient, s, remoteChapters, downloader, storageClient, logger)
				if err != nil {
					logger.Error("failed to process chapters",
						"series", s.Slug,
						"error", err)
					return
				}

				// Prepare metadata update
				localMeta.Title = s.Title
				localMeta.Description = s.Description
				localMeta.Author = s.Author
				localMeta.Status = s.Status
				localMeta.Genres = s.Genres
				localMeta.UpdatedAt = time.Now()

				// Convert remote chapters to disk storage format
				localMeta.Chapters = make([]disk.Chapter, len(remoteChapters))
				for i, chap := range remoteChapters {
					// Preserve existing upload time if chapter exists
					var uploadedAt time.Time
					for _, existingChap := range localMeta.Chapters {
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
					seriesSlug: s.Slug,
					metadata:   localMeta,
				})
				updatesMutex.Unlock()

			}(series)
		}
	}

	// Wait for all series to be processed (downloads are streaming concurrently)
	logger.Info("waiting for all series processing to complete")
	wg.Wait()

	logger.Info("all series processed, downloads are streaming concurrently")

	// Update metadata
	logger.Info("updating metadata", "updates_count", len(pendingUpdates))
	
	var metadataErrors int
	for i, update := range pendingUpdates {
		if err := storageClient.SaveSeriesMetadata(ctx, update.seriesSlug, update.metadata); err != nil {
			metadataErrors++
			logger.Error("failed to save series metadata",
				"series", update.seriesSlug,
				"error", err)
		} else {
			logger.Debug("metadata updated",
				"series", update.seriesSlug,
				"progress", fmt.Sprintf("%d/%d", i+1, len(pendingUpdates)))
		}
	}

	if metadataErrors > 0 {
		logger.Warn("metadata update errors", "count", metadataErrors)
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
		"metadata_updates", len(pendingUpdates)-metadataErrors,
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
				"series", series.Slug,
				"chapter", ch.Number)

			// Fetch page URLs for this chapter
			pages, err := src.FetchPages(ctx, httpClient.Client(), ch)
			if err != nil {
				atomic.AddInt64(&errorCount, 1)
				logger.Error("failed to fetch pages for chapter",
					"series", series.Slug,
					"chapter", ch.Number,
					"error", err)
				return
			}

			if len(pages) == 0 {
				logger.Warn("no pages found for chapter",
					"series", series.Slug,
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
					SeriesSlug:    series.Slug,
					Chapter:       diskChapter,
					Page:          page,
					StorageClient: storageClient,
				})
			}

			atomic.AddInt64(&processedCount, 1)
			logger.Info("chapter pages queued for download",
				"series", series.Slug,
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
		"series", series.Slug,
		"processed", finalProcessed,
		"errors", finalErrors,
		"total", len(remoteChapters))

	if finalErrors > 0 {
		return fmt.Errorf("failed to process %d/%d chapters", finalErrors, len(remoteChapters))
	}

	return nil
}

func setupLogger(level string) *slog.Logger {
	var logLevel slog.Level
	
	switch level {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
}

func setupSignalHandler(cancel context.CancelFunc, logger *slog.Logger) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.Info("received signal, shutting down", "signal", sig)
		cancel()
		
		// Give some time for graceful shutdown
		time.Sleep(2 * time.Second)
		os.Exit(0)
	}()
}

// Helper function to check if a series should be processed based on scrape_only config
func shouldProcessSeries(seriesSlug, scrapeOnly string) bool {
	if scrapeOnly == "" {
		return true
	}
	return seriesSlug == scrapeOnly
}

// Helper function to get minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}