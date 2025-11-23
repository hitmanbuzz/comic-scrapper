package main

import (
	"context"
	"fmt"
	"os"

	"comicrawl/internal/aria2c"
	"comicrawl/internal/cloudflare"
	"comicrawl/internal/config"
	"comicrawl/internal/disk"
	"comicrawl/internal/httpclient"
	"comicrawl/internal/scraper"
	"comicrawl/internal/system"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	newFlags := system.CreateNewFlags()

	// Load configuration
	cfg, err := config.LoadConfig(*newFlags.ConfigPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Parse scrape mode
	var scrapeMode scraper.ScrapeMode
	switch *newFlags.ModeFlag {
	case "full":
		scrapeMode = scraper.ModeFull
	case "incremental":
		scrapeMode = scraper.ModeIncremental
	case "single":
		scrapeMode = scraper.ModeSingle
	default:
		return fmt.Errorf("invalid mode: %s. Must be 'full', 'incremental', or 'single'", *newFlags.ModeFlag)
	}
	logger := system.SetupLogger(cfg, scrapeMode, newFlags)
	logger.UpdateConfigFlags()

	// Validate configuration
	if validationErr := cfg.Validate(); validationErr != nil {
		return fmt.Errorf("invalid configuration: %w", validationErr)
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create shutdown channel for graceful exit
	shutdownCh := make(chan struct{})
	system.SetupSignalHandler(cancel, logger.Logger, shutdownCh)

	logger.ConfigLogging()

	// Initialize storage client
	var storageClient *disk.Client
	storageClient, err = disk.NewClient(ctx, cfg, logger.Logger)
	if err != nil {
		return fmt.Errorf("failed to create disk storage client: %w", err)
	}

	// Only create Cloudflare client if configured
	var flareClient *cloudflare.Client
	if cfg.CloudflareURL != "" {
		flareClient = cloudflare.NewClient(cfg, logger.Logger)
		logger.Logger.Info("Cloudflare client initialized", "url", cfg.CloudflareURL)
	} else {
		logger.Logger.Info("Cloudflare bypass disabled - proceeding without Cloudflare protection bypass")
	}

	// Create a new http client
	httpClient, err := httpclient.NewHTTPClient(cfg, logger.Logger, flareClient)
	if err != nil {
		return fmt.Errorf("failed to create HTTP client: %w", err)
	}

	// Create downloader based on configuration
	var downloader scraper.Downloader

	// Use aria2 from the config
	logger.Logger.Info("using aria2c for streaming downloads", "aria2c_url", cfg.Aria2cURL)
	// Default to 400 workers
	const defaultAria2cWorkers = 400
	aria2cDownloader, err := aria2c.NewDownloader(cfg.Aria2cURL, defaultAria2cWorkers, logger.Logger)
	if err != nil {
		return fmt.Errorf("failed to create aria2c downloader: %w", err)
	}
	downloader = aria2cDownloader
	defer func() {
		if err := aria2cDownloader.Close(); err != nil {
			logger.Logger.Error("failed to close aria2c downloader", "error", err)
		}
	}()

	// Run the scraper with the specified mode
	if err := scraper.RunScraper(ctx, cfg, storageClient, flareClient, httpClient, downloader, logger.Logger, scrapeMode); err != nil {
		return fmt.Errorf("scraper failed: %w", err)
	}

	logger.Logger.Info("scraper completed successfully")
	return nil
}
