package main

import (
	"comicrawl/internal/cloudflare"
	"comicrawl/internal/config"
	"comicrawl/internal/httpclient"
	"comicrawl/internal/registry"
	"comicrawl/internal/system"
	"comicrawl/internal/util/fileio"
	"context"
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

	logger := system.SetupLogger(cfg, newFlags)
	logger.UpdateConfigFlags()

	// Validate configuration
	if validationErr := cfg.Validate(); validationErr != nil {
		fmt.Printf("invalid configuration: %v\n", validationErr)
		return
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	shutdownCh := make(chan struct{})
	system.SetupSignalHandler(cancel, logger.Logger, shutdownCh)

	logger.ConfigLogging()
		
	var flareClient *cloudflare.Client
	if cfg.FlareSolverrURL != "" {
		flareClient = cloudflare.NewFlareClient(cfg, logger.Logger)
		logger.Logger.Info("Cloudflare client initialized", "url", cfg.FlareSolverrURL)
	} else {
		logger.Logger.Info("Cloudflare bypass disabled - proceeding without Cloudflare protection bypass")
	}

	httpClient, err := httpclient.NewHTTPClient(cfg, logger.Logger, nil)
	if err != nil {
		logger.Logger.Error("failed to create HTTP client", "error", err)
		os.Exit(1)
	}

	series := registry.AddSourcesSeries(ctx, cfg, httpClient, flareClient, logger.Logger)

	for _, s := range series {
		if err := fileio.WriteSourceSeries(s, cfg); err != nil {
			logger.Logger.Error("failed to write series JSON", "source", s.GroupName, "error", err)
		}
	}
}
