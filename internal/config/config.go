package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Bucket            string        `yaml:"bucket"`
	Region            string        `yaml:"region"`
	AccessKey         string        `yaml:"access_key"`
	SecretKey         string        `yaml:"secret_key"`
	Endpoint          string        `yaml:"endpoint"`
	FlareSolverrURL   string        `yaml:"flaresolverr_url"`
	UserAgent         string        `yaml:"user_agent"`
	RequestsPerSecond int           `yaml:"requests_per_second"`
	DownloadWorkers   int           `yaml:"download_workers"`
	ScrapeOnly        string        `yaml:"scrape_only"`
	LogLevel          string        `yaml:"log_level"`
	RequestTimeout    time.Duration `yaml:"request_timeout"`
	MaxRetries        int           `yaml:"max_retries"`
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
		cfg.RequestsPerSecond = 5
	}
	if cfg.DownloadWorkers == 0 {
		cfg.DownloadWorkers = 32
	}
	if cfg.RequestTimeout == 0 {
		cfg.RequestTimeout = 30 * time.Second
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}

	return &cfg, nil
}