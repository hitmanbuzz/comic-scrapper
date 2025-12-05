package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"comicrawl/internal/aria2c"
	"comicrawl/internal/cloudflare"
	"comicrawl/internal/config"
	"comicrawl/internal/disk"
	"comicrawl/internal/httpclient"
	"comicrawl/internal/registry"
	"comicrawl/internal/sources"
	"comicrawl/internal/system"
	"log/slog"
)

type TestResult struct {
	SourceName      string
	SeriesSlug      string
	Success         bool
	Error           string
	Duration        time.Duration
	PagesDownloaded int
}

type TestStats struct {
	TotalSources  int
	Successful    int
	Failed        int
	TotalPages    int
	TotalDuration time.Duration
	Results       []TestResult
}

func main() {
	os.Exit(run())
}

func run() int {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <config-file>\n", os.Args[0])
		return 1
	}

	configPath := os.Args[1]

	// Load configuration
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		return 1
	}

	// Override config for testing
	cfg.DryRun = false
	cfg.LimitSeries = 1
	cfg.LimitChapters = 1

	// Setup logger
	logger := system.SetupTestLogger(slog.LevelInfo)

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create temporary test bucket
	testBucket := filepath.Join(cfg.Bucket, "integration-tests")
	cfg.Bucket = testBucket

	// Initialize storage client
	storageClient, err := disk.NewClient(ctx, cfg, logger)
	if err != nil {
		logger.Error("failed to create disk storage client", "error", err)
		return 1
	}

	// Check external services
	if checkErr := checkExternalServices(cfg, logger); checkErr != nil {
		logger.Warn("external services check failed", "error", checkErr)
	}

	// Create downloader
	downloader, err := createDownloader(cfg, logger)
	if err != nil {
		logger.Error("failed to create downloader", "error", err)
		return 1
	}
	defer downloader.Close()

	// Run integration tests
	stats := runIntegrationTests(ctx, cfg, storageClient, downloader, logger)

	// Print results
	printResults(stats)

	// Cleanup
	if err := cleanupTestData(testBucket, logger); err != nil {
		logger.Warn("cleanup failed", "error", err)
	}

	// Return error code if any tests failed
	if stats.Failed > 0 {
		return 1
	}
	return 0
}

func checkExternalServices(cfg *config.Config, logger *slog.Logger) error {
	var errors []string

	// Check aria2c
	// Check aria2c
	logger.Info("checking aria2c service...")
	if err := checkAria2c(cfg.Aria2cURL); err != nil {
		errors = append(errors, fmt.Sprintf("aria2c: %v", err))
	} else {
		logger.Info("aria2c is running")
	}

	// Check FlareSolver
	if cfg.CloudflareURL != "" {
		logger.Info("checking FlareSolver service...")
		if err := checkFlareSolver(cfg.CloudflareURL); err != nil {
			errors = append(errors, fmt.Sprintf("FlareSolver: %v", err))
		} else {
			logger.Info("FlareSolver is running")
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("external service checks failed: %w", errors[0])
	}
	return nil
}

func checkAria2c(url string) error {
	// Simple check - try to connect to aria2c RPC
	client, err := aria2c.NewClient(url, nil)
	if err != nil {
		return err
	}
	defer client.Close()

	// Try to get global stats
	_, err = client.GetGlobalStats()
	return err
}

func checkFlareSolver(url string) error {
	// Simple HTTP check
	// #nosec G107 - URL is from configuration, not input
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 405 {
		return fmt.Errorf("invalid status code: %d", resp.StatusCode)
	}

	return nil
}

func createDownloader(cfg *config.Config, logger *slog.Logger) (interface {
	AddDownload(request aria2c.DownloadRequest)
	Close() error
}, error) {
	logger.Info("using aria2c for integration tests", "aria2c_url", cfg.Aria2cURL)
	return aria2c.NewDownloader(cfg.Aria2cURL, 10, logger)
}

func runIntegrationTests(
	ctx context.Context,
	cfg *config.Config,
	storageClient *disk.Client,
	downloader interface {
		AddDownload(request aria2c.DownloadRequest)
		Close() error
	},
	logger *slog.Logger,
) TestStats {
	sourceList := registry.AddSources(logger)

	if cfg.HasSourceFilters() {
		var filtered []sources.Source
		for _, source := range sourceList {
			if cfg.IsSourceIncluded(source.GetName()) {
				filtered = append(filtered, source)
			}
		}
		sourceList = filtered
	}

	stats := TestStats{
		TotalSources: len(sourceList),
		Results:      make([]TestResult, 0, len(sourceList)),
	}

	logger.Info("starting integration tests", "sources_count", len(sourceList))

	for _, src := range sourceList {
		result := testSource(ctx, cfg, storageClient, downloader, src, logger)
		stats.Results = append(stats.Results, result)

		if result.Success {
			stats.Successful++
			stats.TotalPages += result.PagesDownloaded
		} else {
			stats.Failed++
		}

		stats.TotalDuration += result.Duration

		// Small delay between sources to be respectful
		time.Sleep(100 * time.Millisecond)
	}

	return stats
}

func testSource(
	ctx context.Context,
	cfg *config.Config,
	storageClient *disk.Client,
	downloader interface {
		AddDownload(request aria2c.DownloadRequest)
		Close() error
	},
	src sources.Source,
	logger *slog.Logger,
) TestResult {
	result := TestResult{
		SourceName: src.GetName(),
		Success:    false,
	}

	startTime := time.Now()
	defer func() {
		result.Duration = time.Since(startTime)
	}()

	logger.Info("testing source", "source", src.GetName())

	// Create HTTP client for this source
	httpClient, err := httpclient.NewHTTPClient(cfg, logger, nil)
	if err != nil {
		result.Error = fmt.Sprintf("failed to create HTTP client: %v", err)
		return result
	}

	// Configure for source domain
	var flareClient *cloudflare.Client
	if cfg.CloudflareURL != "" {
		flareClient = cloudflare.NewClient(cfg, logger)
	}
	if configErr := httpClient.ConfigureForDomain(ctx, src.GetBaseURL(), flareClient, cfg.HTTPProxy); configErr != nil {
		result.Error = fmt.Sprintf("failed to configure HTTP client: %v", configErr)
		return result
	}

	// Step 1: List series
	logger.Info("listing series", "source", src.GetName())
	seriesList, err := src.ListSeries(ctx, httpClient.Client())
	if err != nil {
		result.Error = fmt.Sprintf("failed to list series: %v", err)
		return result
	}
	logger.Info("listed series", "source", src.GetName(), "count", len(seriesList.Series))
	if len(seriesList.Series) == 0 {
		result.Error = "no series found"
		return result
	}

	// Pick first series and convert to sources.Series
	scanSeries := seriesList.Series[0]
	series := sources.Series{
		URL:    scanSeries.ComicPageUrl,
		Title:  scanSeries.MainTitle,
		Status: scanSeries.ComicStatus,
	}
	result.SeriesSlug = scanSeries.ComicPageUrl

	logger.Info("selected series", "source", src.GetName(), "url", series.URL, "title", series.Title)

	// Step 2: Fetch chapters
	logger.Info("fetching chapters", "source", src.GetName(), "url", series.URL)
	chapters, err := src.FetchChapters(ctx, httpClient.Client(), series)
	if err != nil {
		result.Error = fmt.Sprintf("failed to fetch chapters: %v", err)
		return result
	}

	if len(chapters) == 0 {
		result.Error = "no chapters found"
		return result
	}

	// Take only first chapter
	chapter := chapters[0]
	logger.Info("selected chapter", "source", src.GetName(), "url", series.URL, "chapter", chapter.Number)

	// Step 3: Fetch pages
	logger.Info("fetching pages", "source", src.GetName(), "url", series.URL, "chapter", chapter.Number)
	pages, err := src.FetchPages(ctx, httpClient.Client(), chapter)
	if err != nil {
		result.Error = fmt.Sprintf("failed to fetch pages: %v", err)
		return result
	}

	if len(pages) == 0 {
		result.Error = "no pages found"
		return result
	}

	logger.Info("found pages", "source", src.GetName(), "url", series.URL, "chapter", chapter.Number, "pages", len(pages))

	// Step 4: Download pages
	logger.Info("downloading pages", "source", src.GetName(), "url", series.URL, "chapter", chapter.Number, "pages", len(pages))

	diskChapter := disk.Chapter{
		Number:    chapter.Number,
		Title:     chapter.Title,
		Pages:     len(pages),
		SourceURL: chapter.URL,
	}

	pagesDownloaded := 0
	for i, page := range pages {
		select {
		case <-ctx.Done():
			result.Error = "context cancelled"
			return result
		default:
		}

		downloader.AddDownload(aria2c.DownloadRequest{
			SeriesSlug:    series.URL,
			Chapter:       diskChapter,
			Page:          page,
			StorageClient: storageClient,
		})
		pagesDownloaded++

		if i%10 == 0 {
			logger.Info("queued pages", "source", src.GetName(), "url", series.URL, "chapter", chapter.Number, "queued", i+1, "total", len(pages))
		}
	}

	result.PagesDownloaded = pagesDownloaded
	result.Success = true

	logger.Info("source test completed successfully - pages queued for download",
		"source", src.GetName(),
		"url", series.URL,
		"chapter", chapter.Number,
		"pages_queued", pagesDownloaded,
		"note", "Check aria2c logs for download confirmation")

	return result
}

func cleanupTestData(bucket string, logger *slog.Logger) error {
	logger.Info("cleaning up test data", "path", bucket)

	return os.RemoveAll(bucket)
}

func printResults(stats TestStats) {
	logger := system.SetupTestLogger(slog.LevelInfo)

	logger.Info("TEST RESULTS")
	logger.Info("test summary",
		"total_sources", stats.TotalSources,
		"successful", stats.Successful,
		"failed", stats.Failed,
		"success_rate", fmt.Sprintf("%.1f%%", float64(stats.Successful)/float64(stats.TotalSources)*100),
		"total_pages", stats.TotalPages,
		"total_duration", stats.TotalDuration,
		"avg_duration", stats.TotalDuration/time.Duration(stats.TotalSources),
	)

	if stats.Failed > 0 {
		logger.Warn("failed sources", "count", stats.Failed)
		for _, result := range stats.Results {
			if !result.Success {
				logger.Error("source test failed",
					"source", result.SourceName,
					"error", result.Error,
				)
			}
		}
	}

	logger.Info("successful sources", "count", stats.Successful)
	for _, result := range stats.Results {
		if result.Success {
			logger.Info("source test passed",
				"source", result.SourceName,
				"series", result.SeriesSlug,
				"pages_downloaded", result.PagesDownloaded,
				"duration", result.Duration,
			)
		}
	}

	if stats.Failed > 0 {
		logger.Error("test completed with failures", "failed_count", stats.Failed)
	} else {
		logger.Info("all tests passed successfully")
	}
}
