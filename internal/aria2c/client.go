package aria2c

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/siku2/arigo"
)

type Client struct {
	arigoClient *arigo.Client
	logger      *slog.Logger
	tempDir     string
}

type DownloadTask struct {
	URL      string
	Filename string
	Headers  map[string]string
}

type DownloadResult struct {
	FilePath string
	Size     int64
	Error    error
}

func NewClient(rpcURL string, logger *slog.Logger) (*Client, error) {
	wsURL := "ws" + rpcURL[4:]
	client, err := arigo.Dial(wsURL, "")
	if err != nil {
		return nil, fmt.Errorf("failed to connect to aria2c RPC: %w", err)
	}

	tempDir, err := os.MkdirTemp("", "comicrawl-aria2c")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	return &Client{
		arigoClient: client,
		logger:      logger,
		tempDir:     tempDir,
	}, nil
}

func (c *Client) Close() error {
	if c.arigoClient != nil {
		c.arigoClient.Close()
	}
	if c.tempDir != "" {
		os.RemoveAll(c.tempDir)
	}
	return nil
}

func (c *Client) DownloadBatch(ctx context.Context, tasks []DownloadTask) ([]DownloadResult, error) {
	results := make([]DownloadResult, len(tasks))
	gids := make([]arigo.GID, len(tasks))

	for i, task := range tasks {
		options := c.createDownloadOptions(task)
		filename := filepath.Join(c.tempDir, task.Filename)

		gid, err := c.arigoClient.AddURI([]string{task.URL}, &options)
		if err != nil {
			results[i] = DownloadResult{Error: fmt.Errorf("failed to add download task: %w", err)}
			continue
		}

		gids[i] = gid
		results[i] = DownloadResult{FilePath: filename}
	}

	for i, gid := range gids {
		if results[i].Error != nil {
			continue
		}

		err := c.waitForDownload(ctx, gid, &results[i])
		if err != nil {
			results[i].Error = err
		}
	}

	return results, nil
}

func (c *Client) createDownloadOptions(task DownloadTask) arigo.Options {
	options := arigo.Options{
		Dir:                    c.tempDir,
		Out:                    task.Filename,
		MaxConnectionPerServer: 16,
		Split:                  16,
		MinSplitSize:           "1M",
		MaxTries:               5,
		RetryWait:              2,
		Timeout:                30,
		MaxDownloadLimit:       "0",
		Referer:                task.URL,
		UserAgent:              "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
	}

	for key, value := range task.Headers {
		switch key {
		case "Referer":
			options.Referer = value
		case "User-Agent":
			options.UserAgent = value
		default:
			if options.Header == "" {
				options.Header = fmt.Sprintf("%s: %s", key, value)
			} else {
				options.Header = options.Header + "\n" + fmt.Sprintf("%s: %s", key, value)
			}
		}
	}

	return options
}

func (c *Client) waitForDownload(ctx context.Context, gid arigo.GID, result *DownloadResult) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if removeErr := c.arigoClient.ForceRemove(gid.String()); removeErr != nil {
				// don't fail - context is already cancelled
			}
			return ctx.Err()
		case <-ticker.C:
			status, err := c.arigoClient.TellStatus(gid.String())
			if err != nil {
				return fmt.Errorf("failed to get download status: %w", err)
			}

			switch status.Status {
			case "complete":
				filePath := status.Files[0].Path
				fileInfo, err := os.Stat(filePath)
				if err != nil {
					return fmt.Errorf("failed to get file info: %w", err)
				}

				result.FilePath = filePath
				result.Size = fileInfo.Size()
				return nil

			case "error":
				return fmt.Errorf("download failed: %s", status.ErrorMessage)

			case "removed":
				return fmt.Errorf("download was removed")
			}
		}
	}
}

func (c *Client) DownloadAndStream(ctx context.Context, task DownloadTask, uploadFunc func(io.Reader) error) error {
	options := c.createDownloadOptions(task)

	gid, err := c.arigoClient.AddURI([]string{task.URL}, &options)
	if err != nil {
		return fmt.Errorf("failed to add download task: %w", err)
	}

	err = c.waitForDownload(ctx, gid, &DownloadResult{})
	if err != nil {
		return err
	}

	filePath := filepath.Join(c.tempDir, task.Filename)
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open downloaded file: %w", err)
	}
	defer file.Close()
	defer os.Remove(filePath)

	return uploadFunc(file)
}

func (c *Client) GetGlobalStats() (arigo.Stats, error) {
	return c.arigoClient.GetGlobalStats()
}

func (c *Client) GetActiveDownloads() ([]arigo.Status, error) {
	return c.arigoClient.TellActive()
}
