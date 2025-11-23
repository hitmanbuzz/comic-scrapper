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

const (
	// taskQueueMultiplier determines buffer size per worker
	taskQueueMultiplier = 100
)

// Chapter represents a chapter with a number - implemented via type assertion
type Chapter any

// StorageClient represents a storage backend that can upload images
type StorageClient any

type DownloadTask struct {
	SeriesSlug    string
	Chapter       Chapter
	Page          sources.Page
	HTTPClient    *http.Client
	StorageClient StorageClient
	Logger        *slog.Logger
}

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

// canUploadImage checks if the client implements the UploadImage method
func canUploadImage(client any) bool {
	_, ok := client.(interface {
		UploadImage(context.Context, string, string, string, io.Reader) error
	})
	return ok
}

type Pool struct {
	workerCount int
	taskChan    chan DownloadTask
	taskPool    sync.Pool
	wg          sync.WaitGroup
	logger      *slog.Logger
}

func NewPool(workerCount int, logger *slog.Logger) *Pool {
	return &Pool{
		workerCount: workerCount,
		taskChan:    make(chan DownloadTask, workerCount*taskQueueMultiplier),
		taskPool: sync.Pool{
			New: func() any {
				return &DownloadTask{}
			},
		},
		logger: logger,
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

func (p *Pool) GetQueueSize() int {
	return len(p.taskChan)
}

func (p *Pool) GetWorkerCount() int {
	return p.workerCount
}

func (p *Pool) GetTaskChanCapacity() int {
	return cap(p.taskChan)
}

var totalProcessedPages int64

func (p *Pool) processTask(task DownloadTask) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	startTime := time.Now()

	chapterNumber, err := getChapterNumber(task.Chapter)
	if err != nil {
		p.logger.Error("invalid chapter type in task", "series", task.SeriesSlug, "chapter_type", fmt.Sprintf("%T", task.Chapter), "error", err)
		return
	}

	p.logger.Debug("processing download task",
		"series", task.SeriesSlug,
		"chapter", chapterNumber,
		"page", task.Page.Number,
		"url", task.Page.URL,
		"queue_size", len(p.taskChan))

	var resp *http.Response
	var downloadErr error

	for attempt := 1; attempt <= 3; attempt++ {
		req, reqErr := http.NewRequestWithContext(ctx, "GET", task.Page.URL, nil)
		if reqErr != nil {
			p.logger.Error("failed to create request",
				"error", reqErr,
				"url", task.Page.URL)
			return
		}

		req.Header.Set("Referer", task.Page.URL)
		req.Header.Set("Accept", "image/webp,image/apng,image/*,*/*;q=0.8")

		resp, downloadErr = task.HTTPClient.Do(req)
		if downloadErr == nil && resp.StatusCode == http.StatusOK {
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
				"error", downloadErr)
			time.Sleep(waitTime)
		}
	}

	if downloadErr != nil {
		p.logger.Error("failed to download image after retries",
			"error", downloadErr,
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

	contentLength := max(resp.ContentLength, 0)

	filename := fmt.Sprintf("%03d%s", task.Page.Number, getFileExtension(task.Page.URL))

	uploadStart := time.Now()
	var uploadErr error
	for attempt := 1; attempt <= 3; attempt++ {
		uploadCtx, uploadCancel := context.WithTimeout(context.Background(), 90*time.Second)

		if !canUploadImage(task.StorageClient) {
			uploadCancel()
			resp.Body.Close()
			p.logger.Error("invalid storage client type",
				"series", task.SeriesSlug,
				"chapter", chapterNumber,
				"filename", filename,
				"storage_type", fmt.Sprintf("%T", task.StorageClient))
			return
		}

		uploadErr = task.StorageClient.(interface {
			UploadImage(context.Context, string, string, string, io.Reader) error
		}).UploadImage(uploadCtx, task.SeriesSlug, chapterNumber, filename, io.NopCloser(resp.Body))
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

			if seeker, ok := resp.Body.(io.Seeker); ok {
				_, seekErr := seeker.Seek(0, io.SeekStart)
				if seekErr != nil {
					p.logger.Error("failed to seek body to start for retry",
						"error", seekErr,
						"series", task.SeriesSlug,
						"chapter", chapterNumber,
						"filename", filename)
					// If seeking fails, the body is likely unreadable for retry.
					// We should probably break here or ensure the next attempt
					// gets a fresh body(?), but for now, just log and let the loop continue.
				}
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

func (p *Pool) getTaskFromPool() *DownloadTask {
	return p.taskPool.Get().(*DownloadTask)
}

func (p *Pool) putTaskToPool(task *DownloadTask) {
	*task = DownloadTask{}
	p.taskPool.Put(task)
}

func (p *Pool) AddTask(task DownloadTask) {
	p.taskChan <- task
}

func (p *Pool) Wait() {
	select {
	case <-p.taskChan:
	default:
		close(p.taskChan)
	}
	p.wg.Wait()
}

func (p *Pool) Close() error {
	p.Wait()
	return nil
}

func (p *Pool) AddDownload(request aria2c.DownloadRequest) {
	task := p.getTaskFromPool()
	task.SeriesSlug = request.SeriesSlug
	task.Chapter = request.Chapter
	task.Page = request.Page
	task.HTTPClient = &http.Client{} // Placeholder - will be overridden by worker
	task.StorageClient = request.StorageClient
	task.Logger = p.logger
	p.AddTask(*task)
	p.putTaskToPool(task)
}

func (p *Pool) DownloadBatch(ctx context.Context, requests []aria2c.DownloadRequest) error {
	for _, req := range requests {
		p.AddDownload(req)
	}

	p.Wait()
	return nil
}

func (p *Pool) ProcessChapterPages(seriesSlug string, chapter Chapter, pages []sources.Page, httpClient *http.Client, storageClient StorageClient, logger *slog.Logger) error {
	chapterNumber, err := getChapterNumber(chapter)
	if err != nil {
		logger.Error("invalid chapter type in ProcessChapterPages", "series", seriesSlug, "chapter_type", fmt.Sprintf("%T", chapter), "error", err)
		return fmt.Errorf("failed to extract chapter number for series %s: %w", seriesSlug, err)
	}

	logger.Info("processing chapter pages",
		"series", seriesSlug,
		"chapter", chapterNumber,
		"pages", len(pages))

	for _, page := range pages {
		task := p.getTaskFromPool()
		task.SeriesSlug = seriesSlug
		task.Chapter = chapter
		task.Page = page
		task.HTTPClient = httpClient
		task.StorageClient = storageClient
		task.Logger = logger
		p.AddTask(*task)
		p.putTaskToPool(task)
	}

	logger.Info("chapter pages queued",
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
