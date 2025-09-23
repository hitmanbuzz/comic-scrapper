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
	Chapter       interface{}
	Page          sources.Page
	StorageClient interface{}
}

func NewDownloader(rpcURL string, logger *slog.Logger) (*Downloader, error) {
	client, err := NewClient(rpcURL, logger)
	if err != nil {
		return nil, err
	}

	downloader := &Downloader{
		client:   client,
		logger:   logger,
		taskChan: make(chan DownloadRequest, 1000), // Large buffer for streaming
	}

	// Start worker goroutines for concurrent downloads
	downloader.Start()

	return downloader, nil
}

func (d *Downloader) Start() {
	// Start multiple workers for concurrent aria2c downloads
	for i := 0; i < 50; i++ { // 50 concurrent aria2c workers
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
			d.logger.Error("download failed", 
				"error", err, 
				"series", task.SeriesSlug,
				"chapter", d.extractChapterNumber(task.Chapter),
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

// AddDownload adds a single download request to the stream
func (d *Downloader) AddDownload(request DownloadRequest) {
	d.taskChan <- request
}

func (d *Downloader) DownloadBatch(ctx context.Context, requests []DownloadRequest) error {
	d.logger.Info("ARIA2C STREAMING DOWNLOAD STARTED", "tasks", len(requests))
	
	// Stream all requests to workers
	for _, req := range requests {
		d.AddDownload(req)
	}
	
	return nil
}

func (d *Downloader) downloadSingle(ctx context.Context, req DownloadRequest) error {
	startTime := time.Now()

	chapterNumber := d.extractChapterNumber(req.Chapter)
	
	// Create unique filename for aria2c
	aria2cFilename := fmt.Sprintf("%s_ch%s_p%03d%s", 
		req.SeriesSlug, 
		chapterNumber, 
		req.Page.Number, 
		getFileExtension(req.Page.URL))
	
	// Final filename for storage
	storageFilename := fmt.Sprintf("%03d%s", req.Page.Number, getFileExtension(req.Page.URL))

	d.logger.Debug("queueing aria2c download",
		"series", req.SeriesSlug,
		"chapter", chapterNumber,
		"page", req.Page.Number,
		"url", req.Page.URL)

	// Download and stream directly to storage
	err := d.client.DownloadAndStream(ctx, DownloadTask{
		URL:      req.Page.URL,
		Filename: aria2cFilename,
		Headers: map[string]string{
			"Referer":    req.Page.URL,
			"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
			"Accept":     "image/webp,image/apng,image/*,*/*;q=0.8",
		},
	}, func(reader io.Reader) error {
		// Upload to storage
		switch client := req.StorageClient.(type) {
		case interface{ UploadImage(context.Context, string, string, string, io.Reader) error }:
			uploadCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
			defer cancel()
			
			return client.UploadImage(uploadCtx, req.SeriesSlug, chapterNumber, storageFilename, io.NopCloser(reader))
		default:
			return fmt.Errorf("invalid storage client type")
		}
	})

	if err != nil {
		return fmt.Errorf("aria2c download failed: %w", err)
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

func (d *Downloader) extractChapterNumber(chapter interface{}) string {
	switch ch := chapter.(type) {
	case struct {
		Number     string
		Title      string
		Pages      int
		UploadedAt time.Time
		SourceURL  string
	}:
		return ch.Number
	case disk.Chapter:
		return ch.Number
	default:
		return "unknown"
	}
}

// Helper function to get file extension from URL
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