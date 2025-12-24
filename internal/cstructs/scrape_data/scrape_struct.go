package scrape_data

// NOTE: Source Provider = Scanlator / Translator Group

// ---------------------------------- Metadata (This will be use by the DataBase to show in the website) -------------------------------------------------
type SourceChapterData struct {
	ChapterNumber float32  `json:"chapter_number"` // Using float is generally better since manga use them in a short chapter
	ChapterTitle  string   `json:"chapter_title"`  // Title for the chapter (optional unless the source provide the title)
	ChapterPath   string   `json:"chapter_path"`   // The path to that specific chapter number directory which contain images
	ChapterImages []string `json:"chapter_images"` // Chapter Images is just all the image files name for the respective chapter number
}

type SourceProviderScrapedData struct {
	Name          string              `json:"name"`                  // Scanlator or Source Provider Name
	TotalChapters int                 `json:"total_chapters"`        // Total chapters release by the scanlator
	SourcePath    string              `json:"source_path"`           // Directory Path to the scanlator where data are stored
	LatestChapter float32             `json:"latest_chapter_number"` // Store the latest chapter number of that comic
	ChapterData   []SourceChapterData `json:"chapter_data"`          // Data for each chapter
}

type MetadataJson struct {
	Title       string                      `json:"comic_title"`     // Title of the comic
	AltTitles   []string                    `json:"alt_titles"`      // Alternative Titles of the comic
	Description string                      `json:"description"`     // Description or synopsis of the comic
	ComicType   string                      `json:"comic_type"`
	MuUrl       string                      `json:"mu_series_url"`   // URL for the comic on MU
	MuSeriesId  int64                       `json:"mu_series_id"`    // Series ID of the comic on MU
	Categories  []string                    `json:"categories"`
	Genres      []string                    `json:"genres"`          // Genres of the comic
	Authors     []string                    `json:"authors"`         // Authors of the comic (Store empty array if not found)
	ComicStatus string                      `json:"comic_status"`    // Current comic status (ongoing, hiatus, completed)
	ReleaseYear string                      `json:"release_year"`    // Release Year of the comic (2022, 2024, etc)
	Thumbnail   string                      `json:"thumnbail_image"` // Thumnbail image of the comic (abc.webp, abc.png, etc)
	ScrapedData []SourceProviderScrapedData `json:"scraped_data"`    // Basic Data for each comic for each source provider
}

// ----------------------------------------- This json data will be stored and only be used by the scraper to know which comic to scrape ------------
type ScanSeriesResponse struct {
	MainTitle    string `json:"title"`     // Title for the comic from the source provider
	ComicPageUrl string `json:"comic_url"` // URL to the source provider comic page
	MuSeriesId   int64  `json:"mu_series_id"`
	Found        bool   `json:"found_mu"`     // This indicates if comic is found in MU or not
	ComicStatus  string `json:"status"`       // Ongoing, Hiatus, Completed
	LastUpdated  int64  `json:"last_updated"` // This is fetch from MU, so we need to run it every 2 times a day so we don't missed the data
}

type FullSeriesResponse struct {
	GroupName   string               `json:"group_name"`   // Source Provider name
	MuGroupIds  []int64              `json:"group_ids"`    // Source Provider IDs from Mangaupdates (can have multiple)
	TotalSeries int                  `json:"total_series"` // Total Series found in scanlator page
	FoundSeries int                  `json:"found_series"`
	Series      []ScanSeriesResponse `json:"series"` // All the Series data from the source provider
}
