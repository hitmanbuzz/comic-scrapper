package aria2c

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"comicrawl/internal/disk"
	"comicrawl/internal/sources"
)

type Downloader struct {
	client   *Client
	logger   *slog.Logger
	counter  int64
	taskChan chan DownloadRequest
	wg       sync.WaitGroup
}

type DownloadRequest struct {
	SeriesSlug    string
	Chapter       Chapter
	Page          sources.Page
	StorageClient StorageClient
}

// Chapter represents a chapter with a number - implemented via type assertion
type Chapter any

// StorageClient represents a storage backend that can upload images
type StorageClient any

// getChapterNumber extracts the chapter number from various chapter types
func getChapterNumber(ch any) (string, error) {
	switch c := ch.(type) {
	case struct {
		Number     string
		Title      string
		Pages      int
		UploadedAt time.Time
		SourceURL  string
	}:
		return c.Number, nil
	case disk.Chapter:
		return c.Number, nil
	default:
		return "", fmt.Errorf("invalid chapter type: %T", c)
	}
}

func NewDownloader(rpcURL string, workerCount int, logger *slog.Logger) (*Downloader, error) {
	client, err := NewClient(rpcURL, logger)
	if err != nil {
		return nil, err
	}

	downloader := &Downloader{
		client:   client,
		logger:   logger,
		taskChan: make(chan DownloadRequest, 1000), // Large buffer for streaming
	}

	downloader.Start(workerCount)

	return downloader, nil
}

func (d *Downloader) Start(workerCount int) {
	for i := 0; i < workerCount; i++ {
		d.wg.Add(1)
		go d.worker(i)
	}
}

func (d *Downloader) worker(id int) {
	defer d.wg.Done()

	d.logger.Debug("aria2c worker started", "worker_id", id)

	for task := range d.taskChan {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		err := d.downloadSingle(ctx, task)
		cancel()

		if err != nil {
			chapterNum, _ := getChapterNumber(task.Chapter)
			d.logger.Error("download failed",
				"error", err,
				"series", task.SeriesSlug,
				"chapter", chapterNum,
				"page", task.Page.Number)
		}
	}

	d.logger.Debug("aria2c worker stopped", "worker_id", id)
}

func (d *Downloader) Close() error {
	close(d.taskChan)
	d.wg.Wait()
	return d.client.Close()
}

func (d *Downloader) AddDownload(request DownloadRequest) {
	d.taskChan <- request
}

func (d *Downloader) DownloadBatch(ctx context.Context, requests []DownloadRequest) error {
	d.logger.Info("ARIA2C STREAMING DOWNLOAD STARTED", "tasks", len(requests))

	for _, req := range requests {
		d.AddDownload(req)
	}

	return nil
}

func (d *Downloader) downloadSingle(ctx context.Context, req DownloadRequest) error {
	startTime := time.Now()

	chapterNumber, err := getChapterNumber(req.Chapter)
	if err != nil {
		return fmt.Errorf("failed to extract chapter number for series %s (chapter_type=%T): %w", req.SeriesSlug, req.Chapter, err)
	}

	aria2cFilename := fmt.Sprintf("%s_ch%s_p%03d%s",
		req.SeriesSlug,
		chapterNumber,
		req.Page.Number,
		getFileExtension(req.Page.URL))

	storageFilename := fmt.Sprintf("%03d%s", req.Page.Number, getFileExtension(req.Page.URL))

	d.logger.Debug("queueing aria2c download",
		"series", req.SeriesSlug,
		"chapter", chapterNumber,
		"page", req.Page.Number,
		"url", req.Page.URL)

	uploadErr := d.client.DownloadAndStream(ctx, DownloadTask{
		URL:      req.Page.URL,
		Filename: aria2cFilename,
		Headers: map[string]string{
			"Referer":    req.Page.URL,
			"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
			"Accept":     "image/webp,image/apng,image/*,*/*;q=0.8",
		},
	}, func(reader io.Reader) error {
		if client, ok := req.StorageClient.(interface {
			UploadImage(context.Context, string, string, string, io.Reader) error
		}); ok {
			uploadCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
			defer cancel()

			return client.UploadImage(uploadCtx, req.SeriesSlug, chapterNumber, storageFilename, io.NopCloser(reader))
		}
		return fmt.Errorf("storage client for series %s (storage_type=%T) does not implement UploadImage method", req.SeriesSlug, req.StorageClient)
	})

	if uploadErr != nil {
		return fmt.Errorf("aria2c download failed: %w", uploadErr)
	}

	duration := time.Since(startTime)
	pageCount := atomic.AddInt64(&d.counter, 1)

	d.logger.Info("successfully downloaded page",
		"series", req.SeriesSlug,
		"chapter", chapterNumber,
		"page", req.Page.Number,
		"filename", storageFilename,
		"duration_ms", duration.Milliseconds(),
		"total_pages_processed", pageCount)

	return nil
}

func getFileExtension(url string) string {
	ext := filepath.Ext(url)
	if ext == "" {
		return ".jpg"
	}
	if ext[0] != '.' {
		ext = "." + ext
	}
	return ext
}
