package system

import (
	"comicrawl/internal/config"
	"comicrawl/internal/scraper"
	"log/slog"
	"os"
	"strings"
)

// Logging
type Logging struct {
	Logger      *slog.Logger
	Cfg         *config.Config
	ScrapMode   scraper.ScrapeMode
	FlagLog     *LogFlagConfig
}

// Create a new logger
func SetupLogger(cfg *config.Config, scrapMode scraper.ScrapeMode, flagLog *LogFlagConfig) *Logging {
	var logLevel slog.Level
	
	switch cfg.LogLevel {
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

	return &Logging{
		Logger: slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: logLevel,
		})),
		Cfg: cfg,
		ScrapMode: scrapMode,
		FlagLog: flagLog,
	}
}

// Show logs for the config
func (l *Logging) ConfigLogging() {
	l.Logger.Info("starting manga scraper", "config", *l.FlagLog.ConfigPath)

	// Log configuration
	l.Logger.Info(
		"configuration",
		"bucket",              l.Cfg.Bucket,
		"storage_type",        l.Cfg.StorageType,
		"use_aria2c",          l.Cfg.UseAria2c,
		"download_workers",    l.Cfg.DownloadWorkers,
		"requests_per_second", l.Cfg.RequestsPerSecond,
		"limit_series",        l.Cfg.LimitSeries,
		"limit_chapters",      l.Cfg.LimitChapters,
		"dry_run",             l.Cfg.DryRun,
		"mode",                l.ScrapMode,
	)

	if l.Cfg.HasSourceFilters() {
		l.Logger.Info("source filters",
			"include_sources", l.Cfg.IncludeSources,
			"exclude_sources", l.Cfg.ExcludeSources,
		)
	}

	if l.Cfg.HasSeriesFilters() {
		l.Logger.Info("series filters",
			"include_series", l.Cfg.IncludeSeries,
			"exclude_series", l.Cfg.ExcludeSeries,
		)
	}
}

func SetupTestLogger(level slog.Level) *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	}))
}

// Override the existing config with the CLI flags
func (l *Logging) UpdateConfigFlags() {
	if *l.FlagLog.SourcesFlag != "" {
		l.Cfg.IncludeSources = strings.Split(*l.FlagLog.SourcesFlag, ",")
		for i := range l.Cfg.IncludeSources {
			l.Cfg.IncludeSources[i] = strings.TrimSpace(l.Cfg.IncludeSources[i])
		}
	}

	if *l.FlagLog.IncludeSeriesFlag != "" {
		l.Cfg.IncludeSeries = strings.Split(*l.FlagLog.IncludeSeriesFlag, ",")
		for i := range l.Cfg.IncludeSeries {
			l.Cfg.IncludeSeries[i] = strings.TrimSpace(l.Cfg.IncludeSeries[i])
		}
	}

	if *l.FlagLog.ExcludeSeriesFlag != "" {
		l.Cfg.ExcludeSeries = strings.Split(*l.FlagLog.ExcludeSeriesFlag, ",")
		for i := range l.Cfg.ExcludeSeries {
			l.Cfg.ExcludeSeries[i] = strings.TrimSpace(l.Cfg.ExcludeSeries[i])
		}
	}

	if *l.FlagLog.LimitSeriesFlag > 0 {
		l.Cfg.LimitSeries = *l.FlagLog.LimitSeriesFlag
	}

	if *l.FlagLog.LimitChaptersFlag > 0 {
		l.Cfg.LimitChapters = *l.FlagLog.LimitChaptersFlag
	}

	if *l.FlagLog.DryRunFlag {
		l.Cfg.DryRun = *l.FlagLog.DryRunFlag
	}
}
