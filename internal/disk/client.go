package disk

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"time"

	"comicrawl/internal/config"
)

type Client struct {
	basePath string
	logger   *slog.Logger
}

type Chapter struct {
	Number     string    `json:"number"`
	Title      string    `json:"title"`
	Pages      int       `json:"pages"`
	UploadedAt time.Time `json:"uploaded_at"`
	SourceURL  string    `json:"source_url"`
}

type SeriesMetadata struct {
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Author      string    `json:"author"`
	Status      string    `json:"status"`
	Genres      []string  `json:"genres"`
	UpdatedAt   time.Time `json:"updated_at"`
	Chapters    []Chapter `json:"chapters"`
}

func NewClient(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*Client, error) {
	basePath := cfg.Bucket
	if basePath == "" {
		return nil, fmt.Errorf("bucket (base path) is required for disk storage")
	}

	err := os.MkdirAll(basePath, 0755)
	if err != nil {
		return nil, fmt.Errorf("failed to create base directory %s: %w", basePath, err)
	}

	logger.Info("disk storage client initialized", "base_path", basePath)

	return &Client{
		basePath: basePath,
		logger:   logger,
	}, nil
}

// NOTE: Remove it once after we fix scrape.go code
func (c *Client) DownloadJSON(ctx context.Context, key string, v any) (bool, error) {
	filePath := path.Join(c.basePath, key)

	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to open JSON file %s: %w", filePath, err)
	}
	defer file.Close()

	if err := json.NewDecoder(file).Decode(v); err != nil {
		return false, fmt.Errorf("failed to decode JSON %s: %w", filePath, err)
	}

	return true, nil
}

// NOTE: Remove it once after we fix scrape.go code
func (c *Client) UploadJSON(ctx context.Context, key string, v any) error {
	filePath := path.Join(c.basePath, key)

	// Create directory if it doesn't exist
	dir := filepath.Dir(filePath)
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	err = os.WriteFile(filePath, data, 0600)
	if err != nil {
		return fmt.Errorf("failed to write JSON file %s: %w", filePath, err)
	}

	c.logger.Debug("uploaded JSON", "path", filePath)
	return nil
}

// NOTE: Remove it once after we fix scrape.go code
func (c *Client) LoadSeriesMetadata(ctx context.Context, seriesSlug string) (*SeriesMetadata, error) {
	key := path.Join(seriesSlug, "meta.json")
	var meta SeriesMetadata

	exists, err := c.DownloadJSON(ctx, key, &meta)
	if err != nil {
		return nil, err
	}

	if !exists {
		return &SeriesMetadata{
			Chapters: []Chapter{},
		}, nil
	}

	return &meta, nil
}

// NOTE: Remove it once after we fix scrape.go code
func (c *Client) SaveSeriesMetadata(ctx context.Context, seriesSlug string, meta *SeriesMetadata) error {
	meta.UpdatedAt = time.Now()
	key := path.Join(seriesSlug, "meta.json")
	return c.UploadJSON(ctx, key, meta)
}
