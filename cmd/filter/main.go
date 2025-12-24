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

	// Check if series_data directory exists
	seriesDataDir := "series_list"
	if _, statErr := os.Stat(seriesDataDir); os.IsNotExist(statErr) {
		logger.Logger.Error("series_data directory doesn't exist", "directory", seriesDataDir)
		os.Exit(1)
	}

	// Regex pattern to match <string>_series.json files
	pattern := regexp.MustCompile(`^.+_series\.json$`)
	var matchingFiles []string

	// Read directory and find matching files
	entries, err := os.ReadDir(seriesDataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading directory: %v\n", err)
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

	// Process each matching file
	for _, filePath := range matchingFiles {
		logger.Logger.Info("processing file", "file", filePath)
		mangaupdates.FilterScanlatorsFromMu(context.Background(), cfg, filePath, httpClient)
	}
}
