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

	"comicrawl/internal/config"
	"comicrawl/internal/disk"
	"comicrawl/internal/flaresolverr"
	"comicrawl/internal/httpclient"
	"comicrawl/internal/sources"
	"comicrawl/internal/worker"
)

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

	flareClient := flaresolverr.NewClient(cfg, logger)
	httpClient, err := httpclient.NewHTTPClient(cfg, logger, flareClient)
	if err != nil {
		logger.Error("failed to create HTTP client", "error", err)
		os.Exit(1)
	}

	// Create worker pool with increased capacity
	downloadWorkers := cfg.DownloadWorkers
	if downloadWorkers < 100 {
		downloadWorkers = 200 // Minimum 200 workers for optimal performance
	}
	workerPool := worker.NewPool(downloadWorkers, logger)
	workerPool.Start()
	defer workerPool.Wait()

	// Start performance monitoring
	startPerformanceMonitoring(ctx, workerPool, logger)

	// Run the scraper
	if err := runScraper(ctx, cfg, storageClient, flareClient, httpClient, workerPool, logger); err != nil {
		logger.Error("scraper failed", "error", err)
		os.Exit(1)
	}

	logger.Info("scraper completed successfully")
}

// startPerformanceMonitoring starts a goroutine that logs performance metrics
func startPerformanceMonitoring(ctx context.Context, workerPool *worker.Pool, logger *slog.Logger) {
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				queueSize := workerPool.GetQueueSize()
				workerCount := workerPool.GetWorkerCount()
				taskChanCapacity := workerPool.GetTaskChanCapacity()

				logger.Info("performance metrics",
					"queue_size", queueSize,
					"workers", workerCount,
					"queue_utilization_pct", float64(queueSize)/float64(taskChanCapacity)*100)
			}
		}
	}()
}

func runScraper(ctx context.Context, cfg *config.Config, storageClient *disk.Client, flareClient *flaresolverr.Client, httpClient *httpclient.HTTPClient, workerPool *worker.Pool, logger *slog.Logger) error {
	startTime := time.Now()
	var totalChapters, totalPages int64

	logger.Info("starting optimized scraper",
		"bucket", cfg.Bucket,
		"workers", cfg.DownloadWorkers,
		"rate_limit", cfg.RequestsPerSecond)

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

	// Process all series and queue downloads first (no blocking S3 operations)
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

		// Process multiple series in parallel
		var seriesWg sync.WaitGroup
		seriesSem := make(chan struct{}, 8) // Limit concurrent series processing

		for _, series := range seriesList {
			if !shouldProcessSeries(series.Slug, cfg.ScrapeOnly) {
				logger.Debug("skipping series", "series", series.Slug)
				continue
			}

			seriesWg.Add(1)
			go func(s sources.Series) {
				defer seriesWg.Done()

				// Acquire series semaphore
				seriesSem <- struct{}{}
				defer func() { <-seriesSem }()

				logger.Info("processing series",
					"source", src.Name(),
					"series", s.Slug,
					"title", s.Title)

				// Load existing metadata (quick operation, kept synchronous)
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

				// Track totals for performance monitoring
				atomic.AddInt64(&totalChapters, int64(len(remoteChapters)))
				for _, ch := range remoteChapters {
					atomic.AddInt64(&totalPages, int64(len(ch.Pages)))
				}

				// Process all chapters in parallel (this is the key optimization)
				if err := processSeriesChaptersParallel(ctx, src, httpClient, s,
					remoteChapters, workerPool, storageClient, logger); err != nil {
					logger.Error("failed to process chapters in parallel",
						"series", s.Slug,
						"error", err)
					return
				}

				// Prepare metadata update (but don't execute yet - that's the bottleneck)
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
						UploadedAt: uploadedAt, // Will be updated by workers after download
						SourceURL:  chap.URL,
					}
				}

				// Add to pending updates (thread-safe)
				updatesMutex.Lock()
				pendingUpdates = append(pendingUpdates, metadataUpdate{
					seriesSlug: s.Slug,
					metadata:   localMeta,
				})
				updatesMutex.Unlock()

			}(series)
		}

		// Wait for all series in this source to be processed
		seriesWg.Wait()
		logger.Info("completed processing source", "source", src.Name())
	}

	logger.Info("all series queued, waiting for downloads to complete",
		"pending_metadata_updates", len(pendingUpdates))

	// Wait for all downloads to complete
	workerPool.Wait()

	logger.Info("all downloads completed, updating metadata",
		"updates_count", len(pendingUpdates))

	// Now batch update all metadata (this is much faster when done at the end)
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
	logger.Info("performance summary",
		"total_time_sec", time.Since(startTime).Seconds(),
		"total_chapters", atomic.LoadInt64(&totalChapters),
		"total_pages", atomic.LoadInt64(&totalPages),
		"chapters_per_sec", float64(atomic.LoadInt64(&totalChapters))/time.Since(startTime).Seconds(),
		"pages_per_sec", float64(atomic.LoadInt64(&totalPages))/time.Since(startTime).Seconds())

	logger.Info("scraper completed successfully",
		"metadata_updates", len(pendingUpdates)-metadataErrors,
		"metadata_errors", metadataErrors)

	return nil
}

// processSeriesChaptersParallel processes chapters in parallel to maximize download throughput
func processSeriesChaptersParallel(ctx context.Context, src sources.Source, httpClient *httpclient.HTTPClient,
	series sources.Series, remoteChapters []sources.Chapter, workerPool *worker.Pool,
	storageClient *disk.Client, logger *slog.Logger) error {

	// Create semaphore to limit concurrent chapter URL fetches
	const maxConcurrentChapters = 25
	sem := make(chan struct{}, maxConcurrentChapters)

	var wg sync.WaitGroup
	var processedCount int64
	var errorCount int64

	logger.Info("starting parallel chapter processing",
		"series", series.Slug,
		"total_chapters", len(remoteChapters),
		"max_concurrent", maxConcurrentChapters)

	for _, chapter := range remoteChapters {
		wg.Add(1)
		go func(ch sources.Chapter) {
			defer wg.Done()

			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

			logger.Debug("fetching pages for chapter",
				"series", series.Slug	,
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

			// Queue all pages immediately
				for _, page := range pages {
					diskChapter := disk.Chapter{
						Number:    ch.Number,
						Title:     ch.Title,
						Pages:     len(pages),
						SourceURL: ch.URL,
					}

					workerPool.AddTask(worker.DownloadTask{
						SeriesSlug:    series.Slug,
						Chapter:       diskChapter,
						Page:          page,
						HTTPClient:    httpClient.Client(),
						StorageClient: storageClient,
						Logger:        logger,
					})
				}

			atomic.AddInt64(&processedCount, 1)
			logger.Info("chapter pages queued",
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

	logger.Info("parallel chapter processing completed",
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