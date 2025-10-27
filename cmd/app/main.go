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
	"comicrawl/internal/worker"
)

func main() {
	newFlags := system.CreateNewFlags()

	// Load configuration
	cfg, err := config.LoadConfig(*newFlags.ConfigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	scrapeMode, err := scraper.ParseScrapeMode(newFlags.ModeFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	logger := system.SetupLogger(cfg, scrapeMode, newFlags)
	logger.UpdateConfigFlags()

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid configuration: %v\n", err)
		os.Exit(1)
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	system.SetupSignalHandler(cancel, logger.Logger)

	logger.ConfigLogging()

	// Initialize storage client
	var storageClient *disk.Client
	switch cfg.StorageType {
	case "disk":
		storageClient, err = disk.NewClient(ctx, cfg, logger.Logger)
		if err != nil {
			logger.Logger.Error("failed to create disk storage client", "error", err)
			os.Exit(1)
		}
	default:
		logger.Logger.Error("unsupported storage type", "storage_type", cfg.StorageType)
		os.Exit(1)
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
		logger.Logger.Error("failed to create HTTP client", "error", err)
		os.Exit(1)
	}

	// Create downloader based on configuration
	var downloader scraper.Downloader

	// Use aria2 from the config
	if cfg.UseAria2c {
		logger.Logger.Info("using aria2c for streaming downloads", "aria2c_url", cfg.Aria2cURL)
		aria2cDownloader, err := aria2c.NewDownloader(cfg.Aria2cURL, cfg.DownloadWorkers*2, logger.Logger)
		if err != nil {
			logger.Logger.Error("failed to create aria2c downloader, falling back to regular pool", "error", err)
			workerPool := worker.NewPool(cfg.DownloadWorkers, logger.Logger)
			workerPool.Start()
			downloader = workerPool
			defer func() {
				if err := workerPool.Close(); err != nil {
					logger.Logger.Error("failed to close worker pool", "error", err)
				}
			}()
		} else {
			downloader = aria2cDownloader
			defer func() {
				if err := aria2cDownloader.Close(); err != nil {
					logger.Logger.Error("failed to close aria2c downloader", "error", err)
				}
			}()
		}
	} else {
		logger.Logger.Info("using regular worker pool for downloads")
		workerPool := worker.NewPool(cfg.DownloadWorkers, logger.Logger)
		workerPool.Start()
		downloader = workerPool
		defer func() {
			if err := workerPool.Close(); err != nil {
				logger.Logger.Error("failed to close worker pool", "error", err)
			}
		}()
	}

	// Run the scraper with the specified mode
	if err := scraper.RunScraper(ctx, cfg, storageClient, flareClient, httpClient, downloader, logger.Logger, scrapeMode); err != nil {
		logger.Logger.Error("scraper failed", "error", err)
		os.Exit(1)
	}

	logger.Logger.Info("scraper completed successfully")
}
