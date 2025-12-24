package validation

import (
	"comicrawl/internal/util"
	"comicrawl/internal/util/metadata"
	"fmt"
)

// NOTE: NEED TO REFACTOR

// IsSeriesScraped checks if a series has been scraped by any group.
// Returns true if the series has metadata with at least one source provider.
func IsSeriesScraped(seriesId int64, seriesIdRootDir string) (bool, error) {
	metadataPath := fmt.Sprintf("%s/%d/metadata.json", seriesIdRootDir, seriesId)

	metadata, err := metadata.ReadMetadata(metadataPath)
	if err != nil {
		return false, fmt.Errorf("read metadata: %w", err)
	}

	return len(metadata.ScrapedData) > 0, nil
}

// IsSeriesScrapedByGroup checks if a series has been scraped by a specific group.
// Returns true if the group name exists in the metadata's scraped data.
func IsSeriesScrapedByGroup(seriesId int64, groupName string, seriesIdRootDir string) (bool, error) {
	metadataPath := fmt.Sprintf("%s/%d/metadata.json", seriesIdRootDir, seriesId)

	metadata, err := metadata.ReadMetadata(metadataPath)
	if err != nil {
		return false, fmt.Errorf("read metadata: %w", err)
	}

	for _, src := range metadata.ScrapedData {
		if src.Name == groupName {
			return true, nil
		}
	}

	return false, nil
}

// IsMetadataFound checks if a metadata.json file exists for a series.
func IsMetadataFound(seriesId int64, seriesIdRootDir string) (bool, error) {
	mPath := fmt.Sprintf("%s/%d/metadata.json", seriesIdRootDir, seriesId)
	return util.IsPathExists(mPath), nil
}

// IsChapterScraped checks if a specific chapter from a series has been scraped by a group.
// Returns true if the chapter number exists in the group's chapter data.
func IsChapterScraped(seriesIDRootDir string, seriesID int64, groupName string, chapterNum float32) (bool, error) {
	metadata, err := metadata.ReadMetadata(fmt.Sprintf("%s/%d/metadata.json", seriesIDRootDir, seriesID))
	if err != nil {
		return false, fmt.Errorf("read metadata: %w", err)
	}

	if metadata.MuSeriesId != seriesID {
		return false, nil
	}

	for _, data := range metadata.ScrapedData {
		if data.Name == groupName {
			for _, c := range data.ChapterData {
				if c.ChapterNumber == chapterNum {
					return true, nil
				}
			}
			return false, nil
		}
	}

	return false, nil
}
