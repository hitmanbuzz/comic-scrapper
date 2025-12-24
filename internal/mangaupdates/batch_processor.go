package mangaupdates

import (
	"comicrawl/internal/cstructs/mu_data"
	"comicrawl/internal/httpclient"
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// BatchOptions configures batch processing behavior.
type BatchOptions struct {
	BatchSize      int
	BatchSleep     time.Duration
	ErrorSleep     time.Duration
	MaxConcurrency int
}

// DefaultBatchOptions returns sensible default batch processing options.
func DefaultBatchOptions() BatchOptions {
	return BatchOptions{
		BatchSize:      10,
		BatchSleep:     100 * time.Millisecond,
		ErrorSleep:     1000 * time.Millisecond,
		MaxConcurrency: 10,
	}
}

// BatchOption is a function that modifies BatchOptions.
type BatchOption func(*BatchOptions)

// WithBatchSize sets the batch size for processing.
func WithBatchSize(size int) BatchOption {
	return func(o *BatchOptions) {
		o.BatchSize = size
	}
}

// WithBatchSleep sets the sleep duration between batches.
func WithBatchSleep(duration time.Duration) BatchOption {
	return func(o *BatchOptions) {
		o.BatchSleep = duration
	}
}

// WithErrorSleep sets the sleep duration after an error.
func WithErrorSleep(duration time.Duration) BatchOption {
	return func(o *BatchOptions) {
		o.ErrorSleep = duration
	}
}

// WithMaxConcurrency sets the maximum number of concurrent goroutines.
func WithMaxConcurrency(max int) BatchOption {
	return func(o *BatchOptions) {
		o.MaxConcurrency = max
	}
}

// BatchResult represents the result of processing a batch of series.
type BatchResult struct {
	SeriesData []AllSeriesData
	Errors     []error
	Processed  int
}

// processSeriesWorker processes a single series and sends the result to the appropriate channel.
func processSeriesWorker(ctx context.Context, client *httpclient.HTTPClient, series mu_data.TitlesStruct, results chan<- AllSeriesData, errors chan<- error, errorSleep time.Duration) {
	lastUpdated := series.LastUpdated.TimeStamp
	seriesData, err := GetSeriesInfo(ctx, series.SeriesId, client)

	if err != nil {
		errors <- fmt.Errorf("series %d: %w", series.SeriesId, err)
		time.Sleep(errorSleep)
		return
	}

	results <- AllSeriesData{
		SeriesData:  seriesData,
		lastUpdated: lastUpdated,
	}
}

// processBatch processes a batch of series titles concurrently.
func processBatch(ctx context.Context, client *httpclient.HTTPClient, batch []mu_data.TitlesStruct, opts BatchOptions) BatchResult {
	var wg sync.WaitGroup
	results := make(chan AllSeriesData, len(batch))
	errors := make(chan error, len(batch))

	// Process each series in the batch
	for _, series := range batch {
		wg.Add(1)
		go func(s mu_data.TitlesStruct) {
			defer wg.Done()
			processSeriesWorker(ctx, client, s, results, errors, opts.ErrorSleep)
		}(series)
	}

	wg.Wait()
	close(results)
	close(errors)

	// Collect results and errors
	var seriesData []AllSeriesData
	var batchErrors []error

	for result := range results {
		seriesData = append(seriesData, result)
	}

	for err := range errors {
		batchErrors = append(batchErrors, err)
	}

	return BatchResult{
		SeriesData: seriesData,
		Errors:     batchErrors,
		Processed:  len(seriesData),
	}
}

// ProcessSeriesTitles processes all series titles using batched concurrent processing.
func ProcessSeriesTitles(ctx context.Context, client *httpclient.HTTPClient, titles []mu_data.TitlesStruct, opts ...BatchOption) ([]AllSeriesData, error) {
	logger := slog.Default()
	options := DefaultBatchOptions()
	for _, opt := range opts {
		opt(&options)
	}

	var allSeries []AllSeriesData
	var allErrors []error
	totalSeries := len(titles)
	processed := 0

	logger.Info("starting series processing", "total_series", totalSeries, "batch_size", options.BatchSize)

	for i := 0; i < totalSeries; i += options.BatchSize {
		end := min(i+options.BatchSize, totalSeries)
		batch := titles[i:end]

		logger.Info("processing batch", "batch", i/options.BatchSize+1, "start", i, "end", end, "batch_size", len(batch))

		result := processBatch(ctx, client, batch, options)
		allSeries = append(allSeries, result.SeriesData...)
		allErrors = append(allErrors, result.Errors...)
		processed += result.Processed

		// Report progress
		progress := float64(processed) / float64(totalSeries) * 100
		logger.Info("batch complete", "processed", processed, "total", totalSeries, "progress", fmt.Sprintf("%.1f%%", progress), "errors", len(result.Errors))

		// Sleep between batches (except after the last batch)
		if end < totalSeries && options.BatchSleep > 0 {
			time.Sleep(options.BatchSleep)
		}
	}

	logger.Info("finished processing all series", "total_processed", len(allSeries), "total_errors", len(allErrors))

	if len(allErrors) > 0 {
		return allSeries, fmt.Errorf("completed with %d errors, first error: %w", len(allErrors), allErrors[0])
	}

	return allSeries, nil
}
