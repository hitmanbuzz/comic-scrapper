package s3

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	
	"comicrawl/config"
)

type Client struct {
	client *s3.Client
	bucket string
	logger *slog.Logger
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
	awsConfig, err := loadAWSConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	s3Client := s3.NewFromConfig(awsConfig, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			// Force path-style for Minio compatibility
			o.UsePathStyle = true
		}
	})

	return &Client{
		client: s3Client,
		bucket: cfg.Bucket,
		logger: logger,
	}, nil
}

func loadAWSConfig(ctx context.Context, cfg *config.Config) (aws.Config, error) {
	var opts []func(*awsconfig.LoadOptions) error

	// Only set credentials if they are provided
	if cfg.AccessKey != "" && cfg.SecretKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		))
	}

	if cfg.Region != "" {
		opts = append(opts, awsconfig.WithRegion(cfg.Region))
	}

	return awsconfig.LoadDefaultConfig(ctx, opts...)
}

func (c *Client) UploadImage(ctx context.Context, seriesSlug, chapterNumber, filename string, data io.Reader) error {
	key := path.Join(seriesSlug, chapterNumber, filename)
	
	_, err := c.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
		Body:   data,
	})
	
	if err != nil {
		return fmt.Errorf("failed to upload image %s: %w", key, err)
	}

	c.logger.Debug("uploaded image", "key", key)
	return nil
}

func (c *Client) DownloadJSON(ctx context.Context, key string, v interface{}) (bool, error) {
	result, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	
	if err != nil {
		var noSuchKey *types.NoSuchKey
		if errorAs(err, &noSuchKey) {
			return false, nil // File doesn't exist
		}
		return false, fmt.Errorf("failed to download JSON %s: %w", key, err)
	}
	defer result.Body.Close()

	if err := json.NewDecoder(result.Body).Decode(v); err != nil {
		return false, fmt.Errorf("failed to decode JSON %s: %w", key, err)
	}

	return true, nil
}

func (c *Client) UploadJSON(ctx context.Context, key string, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	_, err = c.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
		ContentType: aws.String("application/json"),
	})
	
	if err != nil {
		return fmt.Errorf("failed to upload JSON %s: %w", key, err)
	}

	c.logger.Debug("uploaded JSON", "key", key)
	return nil
}

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

func (c *Client) SaveSeriesMetadata(ctx context.Context, seriesSlug string, meta *SeriesMetadata) error {
	meta.UpdatedAt = time.Now()
	key := path.Join(seriesSlug, "meta.json")
	return c.UploadJSON(ctx, key, meta)
}

func (c *Client) LoadChapters(ctx context.Context, seriesSlug string) ([]Chapter, error) {
	key := path.Join(seriesSlug, "chapters.json")
	var chapters []Chapter
	
	exists, err := c.DownloadJSON(ctx, key, &chapters)
	if err != nil {
		return nil, err
	}
	
	if !exists {
		return []Chapter{}, nil
	}
	
	return chapters, nil
}

func (c *Client) SaveChapters(ctx context.Context, seriesSlug string, chapters []Chapter) error {
	key := path.Join(seriesSlug, "chapters.json")
	return c.UploadJSON(ctx, key, chapters)
}

func errorAs(err error, target interface{}) bool {
	// Helper function for type assertion
	if err == nil {
		return false
	}
	if _, ok := err.(interface{ As(interface{}) bool }); ok {
		return errors.As(err, target)
	}
	return false
}