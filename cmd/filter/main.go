package main

import (
	"comicrawl/internal/cloudflare"
	"comicrawl/internal/config"
	"comicrawl/internal/httpclient"
	"comicrawl/internal/mangaupdates"
	"comicrawl/internal/system"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

func main() {
	newFlags := system.CreateNewFlags()

	cfg, err := config.LoadConfig(*newFlags.ConfigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	logger := system.SetupLogger(cfg, newFlags)
	logger.UpdateConfigFlags()

	// Configuring cloudflare bypass with flareclient
	var flareClient *cloudflare.Client
	if cfg.CloudflareURL != "" {
		flareClient = cloudflare.NewClient(cfg, logger.Logger)
		logger.Logger.Info("Cloudflare client initialized", "url", cfg.CloudflareURL)
	} else {
		logger.Logger.Info("Cloudflare bypass disabled - proceeding without Cloudflare protection bypass")
	}

	httpClient, err := httpclient.NewHTTPClient(cfg, logger.Logger, flareClient)
	if err != nil {
		logger.Logger.Error("failed to create HTTP client", "error", err)
		os.Exit(1)
	}

	seriesDataDir := fmt.Sprintf("%s/%s", cfg.LocalDir, cfg.SeriesListDir)
	if _, statErr := os.Stat(seriesDataDir); os.IsNotExist(statErr) {
		logger.Logger.Error("series_data directory doesn't exist", "directory", seriesDataDir)
		os.Exit(1)
	}

	pattern := regexp.MustCompile(`^.+_series\.json$`)
	var matchingFiles []string

	entries, err := os.ReadDir(seriesDataDir)
	if err != nil {
		logger.Logger.Error("Error reading directory | Error: %v\n", err)
		os.Exit(1)
	}

	for _, entry := range entries {
		if !entry.IsDir() && pattern.MatchString(entry.Name()) {
			filePath := filepath.Join(seriesDataDir, entry.Name())
			matchingFiles = append(matchingFiles, filePath)
		}
	}

	if len(matchingFiles) == 0 {
		logger.Logger.Error("no files matching pattern '*_series.json' found", "directory", seriesDataDir)
		os.Exit(1)
	}

	for _, filePath := range matchingFiles {
		logger.Logger.Info("processing file", "file", filePath)
		mangaupdates.FilterScanlatorsFromMu(context.Background(), logger.Logger, cfg, filePath, httpClient)
	}
}
