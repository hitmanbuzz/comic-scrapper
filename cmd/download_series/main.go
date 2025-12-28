package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"comicrawl/internal/cloudflare"
	"comicrawl/internal/config"
	"comicrawl/internal/downloader"
	"comicrawl/internal/httpclient"
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

	logger := system.SetupLogger(cfg, newFlags)
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

	// Only create Cloudflare client if configured
	var flareClient *cloudflare.Client
	if cfg.CloudflareURL != "" {
		flareClient = cloudflare.NewClient(cfg, logger.Logger)
		logger.Logger.Info("Cloudflare client initialized", "url", cfg.CloudflareURL)
	} else {
		logger.Logger.Info("Cloudflare bypass disabled - proceeding without Cloudflare protection bypass")
	}

	httpClient, err := httpclient.NewHTTPClient(cfg, logger.Logger, flareClient)
	if err != nil {
		return fmt.Errorf("failed to create HTTP client: %w", err)
	}

	start := time.Now()

	err = downloader.RunDownload(ctx, logger.Logger, httpClient, cfg, flareClient)
	if err != nil {
	    return err
	}

	logger.Logger.Info("scraper completed successfully")

	end := time.Since(start)
	fmt.Println("Took:", end)
	return nil
}
