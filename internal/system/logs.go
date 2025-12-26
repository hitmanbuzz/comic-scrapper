package system

import (
	"comicrawl/internal/config"
	"log/slog"
	"os"
	"strings"
)

// Logging
type Logging struct {
	Logger    *slog.Logger
	Cfg       *config.Config
	FlagLog   *LogFlagConfig
}

// Create a new logger
func SetupLogger(cfg *config.Config, flagLog *LogFlagConfig) *Logging {
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
		Cfg:       cfg,
		FlagLog:   flagLog,
	}
}

// Show logs for the config
func (l *Logging) ConfigLogging() {
	l.Logger.Info("starting manga scraper", "config", *l.FlagLog.ConfigPath)

	// Log configuration
	l.Logger.Info(
		"configuration",
		"bucket", l.Cfg.Bucket,
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
}
