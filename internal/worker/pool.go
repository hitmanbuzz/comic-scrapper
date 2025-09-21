package worker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"sync"

	"comicrawl/internal/s3"
	"comicrawl/internal/sources"
)

type DownloadTask struct {
	SeriesSlug    string
	Chapter       s3.Chapter
	Page          sources.Page
	HTTPClient    *http.Client
	S3Client      *s3.Client
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
		taskChan:    make(chan DownloadTask, workerCount*2),
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

func (p *Pool) processTask(task DownloadTask) {
	ctx := context.Background()
	
	p.logger.Debug("processing download task", 
		"series", task.SeriesSlug,
		"chapter", task.Chapter.Number,
		"page", task.Page.Number,
		"url", task.Page.URL)

	// Download image
	resp, err := task.HTTPClient.Get(task.Page.URL)
	if err != nil {
		p.logger.Error("failed to download image", 
			"error", err,
			"url", task.Page.URL)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		p.logger.Error("unexpected status code", 
			"status", resp.StatusCode,
			"url", task.Page.URL)
		return
	}

	// Determine filename based on page number
	filename := fmt.Sprintf("%03d%s", task.Page.Number, getFileExtension(task.Page.URL))
	
	// Upload directly to S3
	if err := task.S3Client.UploadImage(ctx, task.SeriesSlug, task.Chapter.Number, filename, resp.Body); err != nil {
		p.logger.Error("failed to upload image to S3", 
			"error", err,
			"series", task.SeriesSlug,
			"chapter", task.Chapter.Number,
			"filename", filename)
		return
	}

	p.logger.Info("successfully processed page", 
		"series", task.SeriesSlug,
		"chapter", task.Chapter.Number,
		"page", task.Page.Number)
}

func (p *Pool) AddTask(task DownloadTask) {
	p.taskChan <- task
}

func (p *Pool) Wait() {
	close(p.taskChan)
	p.wg.Wait()
}

func (p *Pool) ProcessChapterPages(seriesSlug string, chapter s3.Chapter, pages []sources.Page, httpClient *http.Client, s3Client *s3.Client, logger *slog.Logger) error {
	p.logger.Info("processing chapter pages", 
		"series", seriesSlug,
		"chapter", chapter.Number,
		"pages", len(pages))

	for _, page := range pages {
		task := DownloadTask{
			SeriesSlug: seriesSlug,
			Chapter:    chapter,
			Page:       page,
			HTTPClient: httpClient,
			S3Client:   s3Client,
			Logger:     logger,
		}
		p.AddTask(task)
	}

	p.logger.Info("chapter pages queued", 
		"series", seriesSlug,
		"chapter", chapter.Number,
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