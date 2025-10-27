package config

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Bucket            string        `yaml:"bucket"`
	CloudflareURL     string        `yaml:"flaresolverr_url"`
	Aria2cURL         string        `yaml:"aria2c_url" default:"http://localhost:6800/jsonrpc"`
	UserAgent         string        `yaml:"user_agent"`
	HTTPProxy         string        `yaml:"http_proxy"`
	RequestsPerSecond float64       `yaml:"requests_per_second" default:"100"`
	DownloadWorkers   int           `yaml:"download_workers" default:"200"`
	RequestTimeout    time.Duration `yaml:"request_timeout" default:"60s"`
	MaxRetries        int           `yaml:"max_retries" default:"5"`
	LogLevel          string        `yaml:"log_level" default:"info"`
	StorageType       string        `yaml:"storage_type" default:"disk"`
	UseAria2c         bool          `yaml:"use_aria2c" default:"true"`

	// Enhanced filtering options
	IncludeSources []string `yaml:"include_sources"`
	ExcludeSources []string `yaml:"exclude_sources"`
	IncludeSeries  []string `yaml:"include_series"`
	ExcludeSeries  []string `yaml:"exclude_series"`

	// Testing options
	LimitSeries   int  `yaml:"limit_series" default:"0"`
	LimitChapters int  `yaml:"limit_chapters" default:"0"`
	DryRun        bool `yaml:"dry_run" default:"false"`

	// Source configuration
	EnabledSources []string `yaml:"enabled_sources"`
}

// jank to apply default values from struct tags w/ reflection
func setDefaults(cfg *Config) error {
	val := reflect.ValueOf(cfg).Elem()
	typ := val.Type()

	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := typ.Field(i)
		defaultTag := fieldType.Tag.Get("default")

		if defaultTag == "" || !field.IsZero() {
			continue
		}

		switch field.Kind() {
		case reflect.String:
			field.SetString(defaultTag)
		case reflect.Int:
			if v, err := strconv.Atoi(defaultTag); err == nil {
				field.SetInt(int64(v))
			}
		case reflect.Float64:
			if v, err := strconv.ParseFloat(defaultTag, 64); err == nil {
				field.SetFloat(v)
			}
		case reflect.Bool:
			if v, err := strconv.ParseBool(defaultTag); err == nil {
				field.SetBool(v)
			}
		case reflect.Int64:
			// Handle time.Duration
			if field.Type() == reflect.TypeOf(time.Duration(0)) {
				if v, err := time.ParseDuration(defaultTag); err == nil {
					field.SetInt(int64(v))
				}
			}
		}
	}

	return nil
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

	// Apply defaults from struct tags
	if err := setDefaults(&cfg); err != nil {
		return nil, fmt.Errorf("failed to set defaults: %w", err)
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
