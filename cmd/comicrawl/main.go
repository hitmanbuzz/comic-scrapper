package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"comicrawl/internal/config"
	"comicrawl/internal/flaresolverr"
	"comicrawl/internal/httpclient"
	"comicrawl/internal/s3"
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

	// Initialize clients
	s3Client, err := s3.NewClient(ctx, cfg, logger)
	if err != nil {
		logger.Error("failed to create S3 client", "error", err)
		os.Exit(1)
	}

	flareClient := flaresolverr.NewClient(cfg, logger)
	httpClient, err := httpclient.NewHTTPClient(cfg, logger, flareClient)
	if err != nil {
		logger.Error("failed to create HTTP client", "error", err)
		os.Exit(1)
	}

	// Create worker pool
	workerPool := worker.NewPool(cfg.DownloadWorkers, logger)
	workerPool.Start()
	defer workerPool.Wait()

	// Run the scraper
	if err := runScraper(ctx, cfg, s3Client, flareClient, httpClient, workerPool, logger); err != nil {
		logger.Error("scraper failed", "error", err)
		os.Exit(1)
	}

	logger.Info("scraper completed successfully")
}

func runScraper(ctx context.Context, cfg *config.Config, s3Client *s3.Client, flareClient *flaresolverr.Client, httpClient *httpclient.HTTPClient, workerPool *worker.Pool, logger *slog.Logger) error {
	logger.Info("scraper started", 
		"bucket", cfg.Bucket,
		"workers", cfg.DownloadWorkers,
		"rate_limit", cfg.RequestsPerSecond)

	// Initialize sourceList
	sourceList := []sources.Source{
		sources.NewAsuraScans(logger),
		// Add more sources here as they are implemented
	}

	// Process each source
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

		// Process each series
		for _, series := range seriesList {
			if !shouldProcessSeries(series.Slug, cfg.ScrapeOnly) {
				logger.Debug("skipping series", "series", series.Slug)
				continue
			}

			logger.Info("processing series", 
				"source", src.Name(),
				"series", series.Slug,
				"title", series.Title)

			// Fetch existing metadata from S3
			localMeta, err := s3Client.LoadSeriesMetadata(ctx, series.Slug)
			if err != nil {
				logger.Warn("failed to load series metadata from S3", 
					"series", series.Slug,
					"error", err)
				localMeta = &s3.SeriesMetadata{}
			}

			// Fetch chapters from source
			remoteChapters, err := src.FetchChapters(ctx, httpClient.Client(), series)
			if err != nil {
				logger.Error("failed to fetch chapters from source", 
					"series", series.Slug,
					"error", err)
				continue
			}

			// Compare chapters to find new/updated ones
			// For now, treat all chapters as new
			newChapters := remoteChapters
			var updatedChaptersList []sources.Chapter

			if len(newChapters) > 0 || len(updatedChaptersList) > 0 {
				logger.Info("found chapters to process", 
					"series", series.Slug,
					"new", len(newChapters),
					"updated", len(updatedChaptersList))

				// Process new and updated chapters
				allChapters := append(newChapters, updatedChaptersList...)
				for _, chapter := range allChapters {
					// Download chapter pages and upload to S3
				// Create download task for each page in the chapter
				pages, err := src.FetchPages(ctx, httpClient.Client(), chapter)
				if err != nil {
					logger.Error("failed to fetch pages for chapter", 
						"series", series.Slug,
						"chapter", chapter.Number,
						"error", err)
					continue
				}
				
				for _, page := range pages {
					// Find the corresponding chapter in localMeta.Chapters
					var s3Chapter s3.Chapter
					found := false
					for _, localChap := range localMeta.Chapters {
						if localChap.Number == chapter.Number {
							s3Chapter = localChap
							found = true
							break
						}
					}
					
					// If not found, create a new one with default values
					if !found {
						s3Chapter = s3.Chapter{
							Number:     chapter.Number,
							Title:      chapter.Title,
							Pages:      len(chapter.Pages),
							UploadedAt: time.Time{}, // Not uploaded yet
							SourceURL:  chapter.URL,
						}
					}
					
					workerPool.AddTask(worker.DownloadTask{
						SeriesSlug:  series.Slug,
						Chapter:     s3Chapter,
						Page:        page,
						HTTPClient:  httpClient.Client(),
						S3Client:    s3Client,
						Logger:      logger,
					})
				}
					logger.Info("chapter queued for download", 
						"chapter", chapter.Number,
						"title", chapter.Title)
				}

				// Convert remoteChapters to s3.Chapter format and update localMeta.Chapters
				localMeta.Chapters = make([]s3.Chapter, len(remoteChapters))
				for i, chap := range remoteChapters {
					// Check if this chapter already exists in local metadata
					var uploadedAt time.Time
					for _, localChap := range localMeta.Chapters {
						if localChap.Number == chap.Number {
							uploadedAt = localChap.UploadedAt
							break
						}
					}
					
					// If not found or not uploaded yet, use zero time
					if uploadedAt.IsZero() {
						uploadedAt = time.Time{}
					}
					
					localMeta.Chapters[i] = s3.Chapter{
						Number:     chap.Number,
						Title:      chap.Title,
						Pages:      len(chap.Pages),
						UploadedAt: uploadedAt, // Keep existing upload time or use zero time
						SourceURL:  chap.URL,
					}
				}

				// Update metadata with series information
				localMeta.Title = series.Title
				localMeta.Description = series.Description
				localMeta.Author = series.Author
				localMeta.Status = series.Status
				localMeta.Genres = series.Genres
				localMeta.UpdatedAt = time.Now()

				// Save updated metadata
				if err := s3Client.SaveSeriesMetadata(ctx, series.Slug, localMeta); err != nil {
					logger.Error("failed to save series metadata", 
						"series", series.Slug,
						"error", err)
				}
			} else {
				logger.Info("no new chapters found", "series", series.Slug)
			}
		}
	}

	logger.Info("scraper finished processing")
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