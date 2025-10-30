package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"comicrawl/internal/aria2c"
	"comicrawl/internal/config"
	"comicrawl/internal/disk"
	"comicrawl/internal/httpclient"
	"comicrawl/internal/registry"
	"comicrawl/internal/sources"
	"comicrawl/internal/system"
	"comicrawl/internal/worker"
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
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <config-file>\n", os.Args[0])
		os.Exit(1)
	}

	configPath := os.Args[1]

	// Load configuration
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Override config for testing
	cfg.DryRun = false
	cfg.LimitSeries = 1
	cfg.LimitChapters = 1
	cfg.IncludeSources = nil
	cfg.ExcludeSources = nil

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
		os.Exit(1)
	}

	// Check external services
	if err := checkExternalServices(cfg, logger); err != nil {
		logger.Warn("external services check failed", "error", err)
	}

	// Create downloader
	downloader, err := createDownloader(ctx, cfg, logger)
	if err != nil {
		logger.Error("failed to create downloader", "error", err)
		os.Exit(1)
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

	// Exit with error code if any tests failed
	if stats.Failed > 0 {
		os.Exit(1)
	}
}

func checkExternalServices(cfg *config.Config, logger *slog.Logger) error {
	var errors []string

	// Check aria2c
	if cfg.UseAria2c {
		logger.Info("checking aria2c service...")
		if err := checkAria2c(cfg.Aria2cURL); err != nil {
			errors = append(errors, fmt.Sprintf("aria2c: %v", err))
		} else {
			logger.Info("aria2c is running")
		}
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
		return fmt.Errorf("external service checks failed: %v", errors)
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
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("non-200 status code: %d", resp.StatusCode)
	}

	return nil
}

func createDownloader(ctx context.Context, cfg *config.Config, logger *slog.Logger) (interface {
	AddDownload(request aria2c.DownloadRequest)
	Close() error
}, error) {
	if cfg.UseAria2c {
		logger.Info("using aria2c for integration tests", "aria2c_url", cfg.Aria2cURL)
		return aria2c.NewDownloader(cfg.Aria2cURL, 10, logger)
	} else {
		logger.Info("using worker pool for integration tests")
		pool := worker.NewPool(10, logger)
		pool.Start()
		return pool, nil
	}
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
	if err := httpClient.ConfigureForDomain(ctx, src.GetBaseURL(), nil, cfg.HTTPProxy); err != nil {
		result.Error = fmt.Sprintf("failed to configure HTTP client: %v", err)
		return result
	}

	// Step 1: List series
	logger.Info("listing series", "source", src.GetName())
	seriesList, err := src.ListSeries(ctx, httpClient.Client())
	if err != nil {
		result.Error = fmt.Sprintf("failed to list series: %v", err)
		return result
	}

	if len(seriesList) == 0 {
		result.Error = "no series found"
		return result
	}

	// Select first series
	series := seriesList[0]
	result.SeriesSlug = series.Slug

	logger.Info("selected series", "source", src.GetName(), "series", series.Slug, "title", series.Title)

	// Step 2: Fetch chapters
	logger.Info("fetching chapters", "source", src.GetName(), "series", series.Slug)
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
	logger.Info("selected chapter", "source", src.GetName(), "series", series.Slug, "chapter", chapter.Number)

	// Step 3: Fetch pages
	logger.Info("fetching pages", "source", src.GetName(), "series", series.Slug, "chapter", chapter.Number)
	pages, err := src.FetchPages(ctx, httpClient.Client(), chapter)
	if err != nil {
		result.Error = fmt.Sprintf("failed to fetch pages: %v", err)
		return result
	}

	if len(pages) == 0 {
		result.Error = "no pages found"
		return result
	}

	logger.Info("found pages", "source", src.GetName(), "series", series.Slug, "chapter", chapter.Number, "pages", len(pages))

	// Step 4: Download pages
	logger.Info("downloading pages", "source", src.GetName(), "series", series.Slug, "chapter", chapter.Number, "pages", len(pages))

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
			SeriesSlug:    series.Slug,
			Chapter:       diskChapter,
			Page:          page,
			StorageClient: storageClient,
		})
		pagesDownloaded++

		if i%10 == 0 {
			logger.Info("queued pages", "source", src.GetName(), "series", series.Slug, "chapter", chapter.Number, "queued", i+1, "total", len(pages))
		}
	}

	result.PagesDownloaded = pagesDownloaded
	result.Success = true

	logger.Info("source test completed successfully - pages queued for download",
		"source", src.GetName(),
		"series", series.Slug,
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
	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Println("TEST RESULTS")
	fmt.Println(strings.Repeat("=", 50))

	fmt.Printf("\nSummary:\n")
	fmt.Printf("  Total Sources:    %d\n", stats.TotalSources)
	fmt.Printf("  Successful:       %d ✓\n", stats.Successful)
	fmt.Printf("  Failed:           %d ✗\n", stats.Failed)
	fmt.Printf("  Success Rate:     %.1f%%\n", float64(stats.Successful)/float64(stats.TotalSources)*100)
	fmt.Printf("  Total Pages:      %d\n", stats.TotalPages)
	fmt.Printf("  Total Duration:   %v\n", stats.TotalDuration)
	fmt.Printf("  Avg Duration:     %v\n", stats.TotalDuration/time.Duration(stats.TotalSources))

	if stats.Failed > 0 {
		fmt.Printf("\n[!] FAILED SOURCES:\n")
		for _, result := range stats.Results {
			if !result.Success {
				fmt.Printf("  [X] %s: %s\n", result.SourceName, result.Error)
			}
		}
	}

	fmt.Printf("\n[✓] SUCCESSFUL SOURCES:\n")
	for _, result := range stats.Results {
		if result.Success {
			fmt.Printf("  [✓] %s (%s) - %d pages in %v\n",
				result.SourceName,
				result.SeriesSlug,
				result.PagesDownloaded,
				result.Duration)
		}
	}

	fmt.Println("\n" + strings.Repeat("=", 80))

	if stats.Failed > 0 {
		fmt.Printf("\n[X] %d source(s) failed test\n", stats.Failed)
	} else {
		fmt.Printf("\n[✓] All sources passed test!\n")
	}
	fmt.Println(strings.Repeat("=", 80) + "\n")
}
