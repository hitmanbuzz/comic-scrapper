package cstructs

// --------------------- MU Series Last Updated ---------------------
type SeriesLastUpdated struct {
	TimeStamp    int64     `json:"timestamp"`
	AsRfc3339    string    `json:"as_rfc3339"`
	AsString     string    `json:"as_string"`
}

// ---------------------------------- MU Series RSS Feed Custom Format Data ---------------------------
type RssSeriesData struct {
	Title          string
	ChapterData    []RssSeriesChapter
}

type RssSeriesChapter struct {
	Chapter     string
	Scanlator   string
}

// ----------------- MU Group Series Info -----------------
type TitlesStruct struct {
	Title         string             `json:"title"`
	SeriesId      int64              `json:"series_id"`
	SeriesUrl     string             `json:"url"`
	LastUpdated   SeriesLastUpdated  `json:"last_updated"`
}

type GroupSeriesResponse struct {
	ReleaseFrequency   string            `json:"release_frequency"`
	SeriesTitles       []TitlesStruct    `json:"series_titles"`
}

// ---------------- MU Series Info ---------------------
// Main Struct Here
type SeriesResponse struct {
	SeriesId      int64                `json:"series_id"`
	Title         string               `json:"title"`
	URL           string               `json:"url"`
	AltTitles     []SeriesAltTitles    `json:"associated"`
	Description   string               `json:"description"`
	Image         SeriesImage          `json:"image"`     
	ComicType     string               `json:"type"`
	ReleaseYear   string               `json:"year"`
	Genres        []SeriesGenre        `json:"genres"`
	Categories    []SeriesCategories   `json:"categories"`
	ComicStatus   string               `json:"status"`
	Completed     bool                 `json:"completed"`
	Authors       []SeriesAuthors      `json:"authors"`
	Publishers    []SeriesPublishers   `json:"publishers"`
	Publications  []SeriesPublications `json:"publications"` 
	LastUpdated   SeriesLastUpdated    `json:"last_updated"`
	Admin         SeriesAdmin          `json:"admin"`
}

type SeriesAltTitles struct {
	Title    string   `json:"title"`
}

type SeriesImageUrl struct {
	Original    string   `json:"original"`
	Thumbnail   string   `json:"thumb"`
}

type SeriesImage struct {
	URL     SeriesImageUrl  `json:"url"`
	Height  int             `json:"height"`
	Width   int             `json:"width"`
}

type SeriesGenre struct {
	Genre   string  `json:"genre"`
}

type SeriesCategories struct {
	Category     string    `json:"category"`
}

type SeriesAuthors struct {
	AuthorName      string     `json:"name"`
}

type SeriesPublishers struct {
	Name     string     `json:"publisher_name"`
	Id       int64      `json:"publisher_id"`
	Url      string     `json:"url"`
	Type     string     `json:"type"`
	Notes    string     `json:"notes"` 
}

type SeriesPublications struct {
	PublicationName    string      `json:"publication_name"`
	PublisherName      string      `json:"publisher_name"`
	PublisherId        int64       `json:"publisher_id"`
}

type SeriesAdmin struct {
	Approved     bool     `json:"approved"`
}


