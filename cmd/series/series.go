package main

import (
	"comicrawl/internal/cloudflare"
	"comicrawl/internal/config"
	"comicrawl/internal/httpclient"
	"comicrawl/internal/scraper"
	"comicrawl/internal/system"
	"comicrawl/internal/util"
	"fmt"
	"os"
)

func main() {
	newFlags := system.CreateNewFlags()

	cfg, err := config.LoadConfig(*newFlags.ConfigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	logger := system.SetupLogger(cfg, "", newFlags)
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

	// These part is the main thing, the rest above are just copy-pasta from `cmd/app/main.go`
	series := scraper.AddSourcesSeries(httpClient, logger.Logger)

	for _, s := range series {
		util.WriteSourceSeriesJson(s)
	}
}
