package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"comicrawl/internal/aria2c"
	"comicrawl/internal/config"
	"comicrawl/internal/disk"
	"comicrawl/internal/cloudflare"
	"comicrawl/internal/httpclient"
	"comicrawl/internal/sources"
	"comicrawl/internal/worker"
)

// Downloader interface for streaming downloads
type Downloader interface {
	AddDownload(request aria2c.DownloadRequest)
	Close() error
}

type ScrapeMode string

const (
	ModeFull        ScrapeMode = "full"        // Download all content (default)
	ModeIncremental ScrapeMode = "incremental"  // Only new/updated chapters
	ModeSingle      ScrapeMode = "single"      // Specific series only
)

func main() {
	// Parse command line arguments
	configPath := flag.String("config", "config.yaml", "Path to config file")
	modeFlag := flag.String("mode", "full", "Scraping mode: full, incremental, or single")

	// CLI override flags
	sourcesFlag := flag.String("sources", "", "Comma-separated list of sources to include (e.g., 'asurascans,webtoon')")
	includeSeriesFlag := flag.String("include-series", "", "Comma-separated list of series to include")
	excludeSeriesFlag := flag.String("exclude-series", "", "Comma-separated list of series to exclude")
	limitSeriesFlag := flag.Int("limit-series", 0, "Limit number of series to process (0 = no limit)")
	limitChaptersFlag := flag.Int("limit-chapters", 0, "Limit number of chapters per series (0 = no limit)")
	dryRunFlag := flag.Bool("dry-run", false, "Perform a dry run without downloading")

	flag.Parse()

	// Load configuration
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Apply CLI overrides
	if *sourcesFlag != "" {
		cfg.IncludeSources = strings.Split(*sourcesFlag, ",")
		for i := range cfg.IncludeSources {
			cfg.IncludeSources[i] = strings.TrimSpace(cfg.IncludeSources[i])
		}
	}

	if *includeSeriesFlag != "" {
		cfg.IncludeSeries = strings.Split(*includeSeriesFlag, ",")
		for i := range cfg.IncludeSeries {
			cfg.IncludeSeries[i] = strings.TrimSpace(cfg.IncludeSeries[i])
		}
	}

	if *excludeSeriesFlag != "" {
		cfg.ExcludeSeries = strings.Split(*excludeSeriesFlag, ",")
		for i := range cfg.ExcludeSeries {
			cfg.ExcludeSeries[i] = strings.TrimSpace(cfg.ExcludeSeries[i])
		}
	}

	if *limitSeriesFlag > 0 {
		cfg.LimitSeries = *limitSeriesFlag
	}

	if *limitChaptersFlag > 0 {
		cfg.LimitChapters = *limitChaptersFlag
	}

	if *dryRunFlag {
		cfg.DryRun = *dryRunFlag
	}

	// Parse and validate scraping mode
	var mode ScrapeMode
	switch *modeFlag {
	case "full":
		mode = ModeFull
	case "incremental":
		mode = ModeIncremental
	case "single":
		mode = ModeSingle
	default:
		fmt.Fprintf(os.Stderr, "Invalid mode: %s. Must be 'full', 'incremental', or 'single'\n", *modeFlag)
		os.Exit(1)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid configuration: %v\n", err)
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

	// Log configuration
	logger.Info("configuration",
		"bucket", cfg.Bucket,
		"storage_type", cfg.StorageType,
		"use_aria2c", cfg.UseAria2c,
		"download_workers", cfg.DownloadWorkers,
		"requests_per_second", cfg.RequestsPerSecond,
		"limit_series", cfg.LimitSeries,
		"limit_chapters", cfg.LimitChapters,
		"dry_run", cfg.DryRun,
		"mode", mode)

	if cfg.HasSourceFilters() {
		logger.Info("source filters",
			"include_sources", cfg.IncludeSources,
			"exclude_sources", cfg.ExcludeSources)
	}

	if cfg.HasSeriesFilters() {
		logger.Info("series filters",
			"include_series", cfg.IncludeSeries,
			"exclude_series", cfg.ExcludeSeries)
	}

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

	// Only create Cloudflare client if configured
	var flareClient *cloudflare.Client
	if cfg.CloudflareURL != "" {
		flareClient = cloudflare.NewClient(cfg, logger)
		logger.Info("Cloudflare client initialized", "url", cfg.CloudflareURL)
	} else {
		logger.Info("Cloudflare bypass disabled - proceeding without Cloudflare protection bypass")
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
		aria2cDownloader, err := aria2c.NewDownloader(cfg.Aria2cURL, cfg.DownloadWorkers*2, logger)
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

	// Run the scraper with the specified mode
	if err := runScraper(ctx, cfg, storageClient, flareClient, httpClient, downloader, logger, mode); err != nil {
		logger.Error("scraper failed", "error", err)
		os.Exit(1)
	}

	logger.Info("scraper completed successfully")
}

func runScraper(ctx context.Context, cfg *config.Config, storageClient *disk.Client, flareClient *cloudflare.Client, httpClient *httpclient.HTTPClient, downloader Downloader, logger *slog.Logger, mode ScrapeMode) error {
	startTime := time.Now()
	var totalChapters, totalPages int64

	logger.Info("starting streaming scraper",
		"bucket", cfg.Bucket,
		"use_aria2c", cfg.UseAria2c,
		"mode", mode)

	// Collect metadata updates for batch processing at the end
	type metadataUpdate struct {
		seriesSlug string
		metadata   *disk.SeriesMetadata
	}

	var pendingUpdates []metadataUpdate
	var updatesMutex sync.Mutex

	sourceList := []sources.Source{
		sources.NewAsuraScans(logger),
		sources.NewWebtoon(logger),
		sources.NewUtoon(logger),
	}

	// Filter sources based on configuration
	sourceList = filterSources(sourceList, cfg)

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
		seriesCount := 0
		for _, series := range seriesList {
			// Check if we should process this series
			if !shouldProcessSeries(series.Slug, cfg) {
				logger.Debug("skipping series", "series", series.Slug)
				continue
			} else {
				logger.Debug("processing series", "series", series.Slug)
			}

			// Check series limit
			if cfg.LimitSeries > 0 && seriesCount >= cfg.LimitSeries {
				logger.Info("series limit reached", "limit", cfg.LimitSeries)
				break
			}

			seriesCount++

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

				// Handle different scraping modes
				if mode == ModeIncremental || mode == ModeSingle {
					// For incremental mode, skip series that don't exist locally
					if mode == ModeIncremental && (localMeta == nil || len(localMeta.Chapters) == 0) {
						logger.Debug("skipping new series in incremental mode", "series", s.Slug)
						return
					}
					
					// For single mode, only process explicitly included series
					if mode == ModeSingle && !cfg.IsSeriesIncluded(s.Slug) {
						logger.Debug("skipping series not in include list for single mode", "series", s.Slug)
						return
					}
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

				// Apply chapter limit if configured
				if cfg.LimitChapters > 0 && len(remoteChapters) > cfg.LimitChapters {
					logger.Info("limiting chapters", "series", s.Slug, "original", len(remoteChapters), "limited", cfg.LimitChapters)
					remoteChapters = remoteChapters[:cfg.LimitChapters]
				}

				// Filter chapters based on mode
				var chaptersToProcess []sources.Chapter
				if mode == ModeIncremental && localMeta != nil && len(localMeta.Chapters) > 0 {
					// In incremental mode, only process new chapters
					newChapters := findNewChapters(src, localMeta.Chapters, remoteChapters, logger)
					chaptersToProcess = newChapters
					logger.Info("filtering chapters in incremental mode",
						"series", s.Slug,
						"total_remote", len(remoteChapters),
						"new_chapters", len(newChapters))
				} else {
					// In full mode or for new series, process all chapters
					chaptersToProcess = remoteChapters
				}

				if len(chaptersToProcess) == 0 {
					logger.Info("no new chapters to process",
						"series", s.Slug,
						"mode", mode)
					return
				}

				logger.Info("found chapters to process",
					"series", s.Slug,
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
							"series", s.Slug,
							"error", err)
						return
					}
				} else {
					logger.Info("dry-run mode: skipping chapter processing", "series", s.Slug, "chapters", len(chaptersToProcess))
				}

				// Prepare metadata update
				localMeta.Title = s.Title
				localMeta.Description = s.Description
				localMeta.Author = s.Author
				localMeta.Status = s.Status
				localMeta.Genres = s.Genres
				localMeta.UpdatedAt = time.Now()

				// Convert remote chapters to disk storage format (include all chapters)
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

// Helper function to get minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// filterSources filters the source list based on configuration
func filterSources(sourceList []sources.Source, cfg *config.Config) []sources.Source {
	// If no source filters, return all sources
	if !cfg.HasSourceFilters() {
		return sourceList
	}

	var filtered []sources.Source
	for _, source := range sourceList {
		if cfg.IsSourceIncluded(source.Name()) {
			filtered = append(filtered, source)
		}
	}

	return filtered
}

// shouldProcessSeries checks if a series should be processed based on configuration
func findNewChapters(src sources.Source, localChapters []disk.Chapter, remoteChapters []sources.Chapter, logger *slog.Logger) []sources.Chapter {
	// Try to access the BaseSource through type assertion
	switch s := src.(type) {
	case *sources.AsuraScans:
		newChapters, _ := s.BaseSource.CompareChapters(localChapters, remoteChapters)
		return newChapters
	case *sources.Webtoon:
		newChapters, _ := s.BaseSource.CompareChapters(localChapters, remoteChapters)
		return newChapters
	default:
		// Fallback: if we can't type assert, process all chapters
		logger.Debug("unknown source type, processing all chapters", "source", src.Name())
		return remoteChapters
	}
}

func shouldProcessSeries(seriesSlug string, cfg *config.Config) bool {
	included := cfg.IsSeriesIncluded(seriesSlug)

	// Debug logging for series filtering
	if cfg.HasSeriesFilters() {
		// This would be helpful when debugging, but let's not add slog import here
		// Instead, we can rely on the debug logging we added elsewhere
	}

	return included
}
