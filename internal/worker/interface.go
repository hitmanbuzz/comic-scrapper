package worker

import (
	"log/slog"
	"net/http"

	"comicrawl/internal/sources"
)

type PoolInterface interface {
	Start()
	Wait()
	AddTask(task DownloadTask)
	ProcessChapterPages(seriesSlug string, chapter interface{}, pages []sources.Page, httpClient *http.Client, storageClient interface{}, logger *slog.Logger) error
	GetQueueSize() int
	GetWorkerCount() int
	GetTaskChanCapacity() int
}
