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
	CloudflareURL     string        `yaml:"flaresolverr_url"`
	Aria2cURL         string        `yaml:"aria2c_url"`
	UserAgent         string        `yaml:"user_agent"`
	HTTPProxy         string        `yaml:"http_proxy"`
	RequestsPerSecond float64       `yaml:"requests_per_second"`
	RequestTimeout    time.Duration `yaml:"request_timeout"`
	LogLevel          string        `yaml:"log_level"`

	// Enhanced Sources filtering options
	IncludeSources []string `yaml:"include_sources"`
	ExcludeSources []string `yaml:"exclude_sources"`
	IncludeSeries  []string `yaml:"include_series"`
	ExcludeSeries  []string `yaml:"exclude_series"`

	// Testing options
	LimitSeries   int  `yaml:"limit_series"`
	LimitChapters int  `yaml:"limit_chapters"`
	DryRun        bool `yaml:"dry_run"`
}

func NewDefaultConfig() *Config {
	return &Config{
		Aria2cURL:         "http://localhost:6800/jsonrpc",
		RequestsPerSecond: 100,
		RequestTimeout:    60 * time.Second,
		LogLevel:          "info",
		LimitSeries:       0,
		LimitChapters:     0,
		DryRun:            false,
	}
}

func LoadConfig(configPath string) (*Config, error) {
	if configPath == "" {
		configPath = "config.yaml"
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	cfg := NewDefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return cfg, nil
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.Bucket == "" {
		return fmt.Errorf("bucket path is required")
	}

	if c.RequestTimeout <= 0 {
		return fmt.Errorf("request_timeout must be positive")
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
