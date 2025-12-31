package download_data

type DownloadData struct {
    // Scanlator URL
    ScanURL string `json:"scan_url"`
    // Scanlator Name
    ScanName string `json:"scan_name"`
    // Number of series scrape
    TotalSeries int `json:"total_series"`
    // Number of pages(images) throughout all the series
    TotalImages int64 `json:"total_images"`
    Series []SeriesData `json:"series"`
}

type SeriesData struct {
    SeriesID int64 `json:"series_id"`
    SeriesName string `json:"series_name"`
    SeriesURL string `json:"series_url"`
    // Number of chapters the series have
    TotalChapter int `json:"total_chapter"`
    // Number of images the chapter have
    TotalImages int64 `json:"total_images"`
    Chapter []ChapterData `json:"chapter"`
}

type ChapterData struct {
    ChapterNumber float32 `json:"chapter_number"`
    ChapterURL string `json:"chapter_url"`
    ChapterName string `json:"chapter_name"`
    // Number of images the chapter have
    TotalImages int64 `json:"total_images"`
    Image []ImageData `json:"image"`
}

type ImageData struct {
    ImagerNumber int `json:"image_number"`
    ImageURL string `json:"image_url"`
}

