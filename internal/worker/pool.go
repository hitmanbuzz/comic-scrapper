package worker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"sync"
	"sync/atomic"
	"time"

	"comicrawl/internal/aria2c"
	"comicrawl/internal/disk"
	"comicrawl/internal/sources"
)

type DownloadTask struct {
	SeriesSlug    string
	Chapter       interface{}
	Page          sources.Page
	HTTPClient    *http.Client
	StorageClient interface{}
	Logger        *slog.Logger
}

type Pool struct {
	workerCount int
	taskChan    chan DownloadTask
	wg          sync.WaitGroup
	logger      *slog.Logger
}

func NewPool(workerCount int, logger *slog.Logger) *Pool {
	return &Pool{
		workerCount: workerCount,
		taskChan:    make(chan DownloadTask, workerCount*20), // Much larger buffer
		logger:      logger,
	}
}

func (p *Pool) Start() {
	for i := 0; i < p.workerCount; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
}

func (p *Pool) worker(id int) {
	defer p.wg.Done()
	
	p.logger.Debug("worker started", "worker_id", id)
	
	for task := range p.taskChan {
		p.processTask(task)
	}
	
	p.logger.Debug("worker stopped", "worker_id", id)
}

// GetQueueSize returns the current number of queued tasks
func (p *Pool) GetQueueSize() int {
	return len(p.taskChan)
}

// GetWorkerCount returns the number of workers
func (p *Pool) GetWorkerCount() int {
	return p.workerCount
}

// GetTaskChanCapacity returns the capacity of the task channel
func (p *Pool) GetTaskChanCapacity() int {
	return cap(p.taskChan)
}

var totalProcessedPages int64

func (p *Pool) processTask(task DownloadTask) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	startTime := time.Now()

	// Extract chapter number from the interface{}
	var chapterNumber string
	switch ch := task.Chapter.(type) {
	case struct {
		Number     string
		Title      string
		Pages      int
		UploadedAt time.Time
		SourceURL  string
	}:
		chapterNumber = ch.Number
	case disk.Chapter:
		chapterNumber = ch.Number
	default:
		p.logger.Error("invalid chapter type in task")
		return
	}

	p.logger.Debug("processing download task",
		"series", task.SeriesSlug,
		"chapter", chapterNumber,
		"page", task.Page.Number,
		"url", task.Page.URL,
		"queue_size", len(p.taskChan))

	// Download image with retries
	var resp *http.Response
	var err error

	for attempt := 1; attempt <= 3; attempt++ {
		req, reqErr := http.NewRequestWithContext(ctx, "GET", task.Page.URL, nil)
		if reqErr != nil {
			p.logger.Error("failed to create request",
				"error", reqErr,
				"url", task.Page.URL)
			return
		}

		// Add headers to avoid being blocked
		req.Header.Set("Referer", task.Page.URL)
		req.Header.Set("Accept", "image/webp,image/apng,image/*,*/*;q=0.8")

		resp, err = task.HTTPClient.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			break
		}

		if resp != nil {
			resp.Body.Close()
		}

		if attempt < 3 {
			waitTime := time.Duration(attempt) * 500 * time.Millisecond
			p.logger.Debug("retrying download",
				"attempt", attempt,
				"wait", waitTime,
				"url", task.Page.URL,
				"error", err)
			time.Sleep(waitTime)
		}
	}

	if err != nil {
		p.logger.Error("failed to download image after retries",
			"error", err,
			"url", task.Page.URL)
		return
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		p.logger.Error("unexpected status code",
			"status", resp.StatusCode,
			"url", task.Page.URL)
		return
	}

	// Get content length for monitoring
	contentLength := resp.ContentLength
	if contentLength < 0 {
		contentLength = 0
	}

	// Determine filename based on page number
	filename := fmt.Sprintf("%03d%s", task.Page.Number, getFileExtension(task.Page.URL))

	// Upload directly to storage with streaming and retries
	uploadStart := time.Now()
	var uploadErr error
	for attempt := 1; attempt <= 3; attempt++ {
		uploadCtx, uploadCancel := context.WithTimeout(context.Background(), 90*time.Second)
		
		// Type assert the storage client
		switch client := task.StorageClient.(type) {
		case interface{ UploadImage(context.Context, string, string, string, io.Reader) error }:
			uploadErr = client.UploadImage(uploadCtx, task.SeriesSlug, chapterNumber, filename, io.NopCloser(resp.Body))
		default:
			uploadErr = fmt.Errorf("invalid storage client type")
		}
		uploadCancel()
		
		if uploadErr == nil {
			break
		}
		
		if attempt < 3 {
			waitTime := time.Duration(attempt) * 2 * time.Second
			p.logger.Debug("retrying storage upload",
				"attempt", attempt,
				"wait", waitTime,
				"series", task.SeriesSlug,
				"chapter", chapterNumber,
				"filename", filename,
				"error", uploadErr)
			time.Sleep(waitTime)
			
			// Reset body position for retry
			if seeker, ok := resp.Body.(io.Seeker); ok {
				seeker.Seek(0, io.SeekStart)
			}
		}
	}
	
	if uploadErr != nil {
		resp.Body.Close()
		p.logger.Error("failed to upload image to storage after retries",
			"error", uploadErr,
			"series", task.SeriesSlug,
			"chapter", chapterNumber,
			"filename", filename)
		return
	}
	resp.Body.Close()

	uploadDuration := time.Since(uploadStart)
	totalDuration := time.Since(startTime)

	// Increment processed pages counter
	pageCount := atomic.AddInt64(&totalProcessedPages, 1)

	p.logger.Info("successfully processed page",
			"series", task.SeriesSlug,
			"chapter", chapterNumber,
			"page", task.Page.Number,
			"size_bytes", contentLength,
			"download_ms", totalDuration.Milliseconds()-uploadDuration.Milliseconds(),
			"upload_ms", uploadDuration.Milliseconds(),
			"total_ms", totalDuration.Milliseconds(),
			"queue_remaining", len(p.taskChan),
			"total_pages_processed", pageCount)
}

func (p *Pool) AddTask(task DownloadTask) {
	p.taskChan <- task
}

func (p *Pool) Wait() {
	select {
	case <-p.taskChan:
		// Channel already closed
	default:
		close(p.taskChan)
	}
	p.wg.Wait()
}

// Close implements the downloader interface
func (p *Pool) Close() error {
	p.Wait()
	return nil
}

// AddDownload implements the downloader interface for compatibility
func (p *Pool) AddDownload(request aria2c.DownloadRequest) {
	task := DownloadTask{
		SeriesSlug:    request.SeriesSlug,
		Chapter:       request.Chapter,
		Page:          request.Page,
		HTTPClient:    &http.Client{}, // Placeholder - will be overridden by worker
		StorageClient: request.StorageClient,
		Logger:        p.logger,
	}
	p.AddTask(task)
}

// DownloadBatch implements the downloader interface for compatibility
func (p *Pool) DownloadBatch(ctx context.Context, requests []aria2c.DownloadRequest) error {
	// Convert aria2c download requests to worker pool tasks
	for _, req := range requests {
		p.AddDownload(req)
	}
	
	p.Wait()
	return nil
}

func (p *Pool) ProcessChapterPages(seriesSlug string, chapter interface{}, pages []sources.Page, httpClient *http.Client, storageClient interface{}, logger *slog.Logger) error {
	// Extract chapter number from the interface{}
	var chapterNumber string
	switch ch := chapter.(type) {
	case struct {
		Number     string
		Title      string
		Pages      int
		UploadedAt time.Time
		SourceURL  string
	}:
		chapterNumber = ch.Number
	case disk.Chapter:
		chapterNumber = ch.Number
	default:
		p.logger.Error("invalid chapter type in ProcessChapterPages")
		return fmt.Errorf("invalid chapter type")
	}

	p.logger.Info("processing chapter pages", 
		"series", seriesSlug,
		"chapter", chapterNumber,
		"pages", len(pages))

	for _, page := range pages {
		task := DownloadTask{
			SeriesSlug:    seriesSlug,
			Chapter:       chapter,
			Page:          page,
			HTTPClient:    httpClient,
			StorageClient: storageClient,
			Logger:        logger,
		}
		p.AddTask(task)
	}

	p.logger.Info("chapter pages queued", 
		"series", seriesSlug,
		"chapter", chapterNumber,
		"pages", len(pages))
	return nil
}

func getFileExtension(url string) string {
	// Extract file extension from URL
	ext := path.Ext(url)
	if ext == "" {
		return ".jpg" // default extension
	}
	
	// Ensure extension starts with a dot
	if ext[0] != '.' {
		ext = "." + ext
	}
	
	return ext
}

// Helper function to stream download without loading entire file into memory
func streamDownload(ctx context.Context, client *http.Client, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return resp.Body, nil
}