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
	HTTPProxy         string        `yaml:"http_proxy"`
	RequestsPerSecond float64       `yaml:"requests_per_second" default:"100"`
	DownloadWorkers   int           `yaml:"download_workers" default:"200"`
	RequestTimeout    time.Duration `yaml:"request_timeout" default:"30s"`
	MaxRetries        int           `yaml:"max_retries" default:"3"`
	ScrapeOnly        string        `yaml:"scrape_only"`
	LogLevel          string        `yaml:"log_level"`
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