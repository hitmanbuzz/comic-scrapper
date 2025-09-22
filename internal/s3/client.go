package s3

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"path"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	v4 "github.com/aws/aws-sdk-go/aws/signer/v4"
	"github.com/aws/aws-sdk-go/service/s3"

	"comicrawl/internal/config"
)

type Client struct {
	client *s3.S3
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
	awsConfig := &aws.Config{
		Region: aws.String(cfg.Region),
		Credentials: credentials.NewStaticCredentials(
			cfg.AccessKey,
			cfg.SecretKey,
			"", // token
		),
		S3ForcePathStyle: aws.Bool(true),
	}

	if cfg.Endpoint != "" {
		awsConfig.Endpoint = aws.String(cfg.Endpoint)
	}

	// Create session
	sess, err := session.NewSession(awsConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS session: %w", err)
	}

	// Create S3 client
	s3Client := s3.New(sess)

	s3Client.Handlers.Sign.SwapNamed(request.NamedHandler{
		Name: "v4.SignRequestHandler",
		Fn: func(r *request.Request) {
			signer := v4.NewSigner(r.Config.Credentials)
			signer.DisableRequestBodyOverwrite = true
			signer.UnsignedPayload = true
			signer.Sign(r.HTTPRequest, nil, r.ClientInfo.ServiceName, *r.Config.Region, r.Time)
		},
	})

	return &Client{
		client: s3Client,
		bucket: cfg.Bucket,
		logger: logger,
	}, nil
}

func (c *Client) UploadImage(ctx context.Context, seriesSlug, chapterNumber, filename string, data io.Reader) error {
	key := path.Join(seriesSlug, chapterNumber, filename)
	
	var body io.ReadSeekCloser
	var contentLength *int64
	
	// Check if the reader is already a seeker
	if seeker, ok := data.(io.ReadSeeker); ok {
		// Get the size by seeking to the end
		size, err := seeker.Seek(0, io.SeekEnd)
		if err != nil {
			return fmt.Errorf("failed to seek to end: %w", err)
		}
		contentLength = &size
		
		// Reset to beginning
		if _, err := seeker.Seek(0, io.SeekStart); err != nil {
			return fmt.Errorf("failed to reset reader position: %w", err)
		}
		
		// Convert to ReadSeekCloser
		if closer, ok := seeker.(io.ReadSeekCloser); ok {
			body = closer
		} else {
			body = aws.ReadSeekCloser(seeker)
		}
	} else {
		// If not a seeker, we need to read all data into memory first
		// This ensures we know the content length
		buf, err := io.ReadAll(data)
		if err != nil {
			return fmt.Errorf("failed to read data: %w", err)
		}
		
		size := int64(len(buf))
		contentLength = &size
		body = aws.ReadSeekCloser(bytes.NewReader(buf))
	}
	
	input := &s3.PutObjectInput{
		Bucket:        aws.String(c.bucket),
		Key:           aws.String(key),
		Body:          body,
		ContentLength: contentLength, // Always set content length
	}
	
	_, err := c.client.PutObjectWithContext(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to upload image %s: %w", key, err)
	}

	c.logger.Debug("uploaded image", "key", key, "size", *contentLength)
	return nil
}

func (c *Client) DownloadJSON(ctx context.Context, key string, v interface{}) (bool, error) {
	result, err := c.client.GetObjectWithContext(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case s3.ErrCodeNoSuchKey:
				return false, nil // File doesn't exist
			}
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

	_, err = c.client.PutObjectWithContext(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
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