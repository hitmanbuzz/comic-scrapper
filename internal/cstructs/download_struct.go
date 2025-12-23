package cstructs

import "io"

const MAX_SERIES int = 5
const MAX_PAGES int = 50

type DownloadPageData struct {
    PageNumber int
    ImageBody io.Reader
    ImageExt string
}

type DownloadChapterData struct {
    ChapterNumber float32
    // Contain all the pages url for the chapter (key = image page number | value = image response body)
    ImagesData []DownloadPageData
}

type DownloadSeriesData struct {
    // The scanlator name
    SourceProvider string
    // Series ID found in MU
    SeriesID int64
    ChapterData []DownloadChapterData
}

type Downloader struct {
    // The root directory for the scrapped location
    BaseDownloadDir string
    // Number of download processess at a time
    BatchProcess int64
    // Will have the limit of `BatchProcess` amount in the array
    DownloadBatch [MAX_SERIES]DownloadSeriesData
    // Will store dynamic size of series to be downloaded
    //
    // Will collect every `MAX_SIZE` data and insert to `DownloadBatch`
    QueueBatch []DownloadSeriesData
}
