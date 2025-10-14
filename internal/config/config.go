package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Bucket            string        `yaml:"bucket"`
	CloudflareURL     string        `yaml:"cloudflare_url"`
	Aria2cURL         string        `yaml:"aria2c_url" default:"http://localhost:6800/jsonrpc"`
	UserAgent         string        `yaml:"user_agent"`
	HTTPProxy         string        `yaml:"http_proxy"`
	RequestsPerSecond float64       `yaml:"requests_per_second" default:"100"`
	DownloadWorkers   int           `yaml:"download_workers" default:"200"`
	RequestTimeout    time.Duration `yaml:"request_timeout" default:"60s"`
	MaxRetries        int           `yaml:"max_retries" default:"5"`
	LogLevel          string        `yaml:"log_level"`
	StorageType       string        `yaml:"storage_type" default:"disk"`
	UseAria2c         bool          `yaml:"use_aria2c" default:"true"`
	
	// Enhanced filtering options
	IncludeSources    []string      `yaml:"include_sources"`
	ExcludeSources    []string      `yaml:"exclude_sources"`
	IncludeSeries     []string      `yaml:"include_series"`
	ExcludeSeries     []string      `yaml:"exclude_series"`
	
	// Testing options
	LimitSeries       int           `yaml:"limit_series" default:"0"`
	LimitChapters     int           `yaml:"limit_chapters" default:"0"`
	DryRun            bool          `yaml:"dry_run" default:"false"`
	
	// Source configuration
	EnabledSources    []string      `yaml:"enabled_sources"`
}

func LoadConfig(configPath string) (*Config, error) {
	if configPath == "" {
		configPath = "config.yaml"
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Set defaults
	if cfg.RequestsPerSecond == 0 {
		cfg.RequestsPerSecond = 100
	}
	if cfg.DownloadWorkers == 0 {
		cfg.DownloadWorkers = 200
	}
	if cfg.RequestTimeout == 0 {
		cfg.RequestTimeout = 60 * time.Second
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 5
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	if cfg.Aria2cURL == "" {
		cfg.Aria2cURL = "http://localhost:6800/jsonrpc"
	}
	if cfg.StorageType == "" {
		cfg.StorageType = "disk"
	}

	return &cfg, nil
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.Bucket == "" {
		return fmt.Errorf("bucket path is required")
	}
	
	if c.StorageType != "disk" {
		return fmt.Errorf("unsupported storage type: %s", c.StorageType)
	}
	
	if c.RequestsPerSecond <= 0 {
		return fmt.Errorf("requests_per_second must be positive")
	}
	
	if c.DownloadWorkers <= 0 {
		return fmt.Errorf("download_workers must be positive")
	}
	
	if c.RequestTimeout <= 0 {
		return fmt.Errorf("request_timeout must be positive")
	}
	
	if c.MaxRetries < 0 {
		return fmt.Errorf("max_retries cannot be negative")
	}
	
	if c.LimitSeries < 0 {
		return fmt.Errorf("limit_series cannot be negative")
	}
	
	if c.LimitChapters < 0 {
		return fmt.Errorf("limit_chapters cannot be negative")
	}
	
	// Validate log level
	validLogLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}
	
	if c.LogLevel != "" && !validLogLevels[strings.ToLower(c.LogLevel)] {
		return fmt.Errorf("invalid log level: %s", c.LogLevel)
	}
	
	return nil
}

// HasSourceFilters returns true if any source filtering is configured
func (c *Config) HasSourceFilters() bool {
	return len(c.IncludeSources) > 0 || len(c.ExcludeSources) > 0
}

// HasSeriesFilters returns true if any series filtering is configured
func (c *Config) HasSeriesFilters() bool {
	return len(c.IncludeSeries) > 0 || len(c.ExcludeSeries) > 0
}

// IsSourceIncluded checks if a source should be included based on filters
func (c *Config) IsSourceIncluded(sourceName string) bool {
	// If no source filters, include all sources
	if !c.HasSourceFilters() {
		return true
	}
	
	// If include sources are specified, only include those
	if len(c.IncludeSources) > 0 {
		for _, include := range c.IncludeSources {
			if strings.EqualFold(include, sourceName) {
				return true
			}
		}
		return false
	}
	
	// If exclude sources are specified, exclude those
	if len(c.ExcludeSources) > 0 {
		for _, exclude := range c.ExcludeSources {
			if strings.EqualFold(exclude, sourceName) {
				return false
			}
		}
	}
	
	return true
}

// IsSeriesIncluded checks if a series should be included based on filters
func (c *Config) IsSeriesIncluded(seriesSlug string) bool {
	// If no series filters, include all series
	if !c.HasSeriesFilters() {
		return true
	}
	
	// Check exclude list first
	for _, exclude := range c.ExcludeSeries {
		if strings.Contains(seriesSlug, exclude) || seriesSlug == exclude {
			return false
		}
	}
	
	// If include list is empty, include all non-excluded series
	if len(c.IncludeSeries) == 0 {
		return true
	}
	
	// Check include list
	for _, include := range c.IncludeSeries {
		if strings.Contains(seriesSlug, include) || seriesSlug == include {
			return true
		}
	}
	
	// If include list is specified but series is not in it, don't include
	return false
}
