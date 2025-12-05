package validation

import (
	"comicrawl/internal/cstructs"
	"encoding/json"
	"fmt"
	"os"
)

// IsSeriesScraped checks if a series has been scraped by any group.
// Returns true if the series has metadata with at least one source provider.
func IsSeriesScraped(seriesId int64, seriesIdRootDir string) (bool, error) {
	metadataPath := fmt.Sprintf("%s/%d/metadata.json", seriesIdRootDir, seriesId)

	metadata, err := ReadMetadata(metadataPath)
	if err != nil {
		return false, fmt.Errorf("read metadata: %w", err)
	}

	return len(metadata.ScrapedData) > 0, nil
}

// IsSeriesScrapedByGroup checks if a series has been scraped by a specific group.
// Returns true if the group name exists in the metadata's scraped data.
func IsSeriesScrapedByGroup(seriesId int64, groupName string, seriesIdRootDir string) (bool, error) {
	metadataPath := fmt.Sprintf("%s/%d/metadata.json", seriesIdRootDir, seriesId)

	metadata, err := ReadMetadata(metadataPath)
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
	return PathExists(mPath), nil
}

// IsChapterScraped checks if a specific chapter from a series has been scraped by a group.
// Returns true if the chapter number exists in the group's chapter data.
func IsChapterScraped(seriesIDRootDir string, seriesID int64, groupName string, chapterNum float32) (bool, error) {
	metadata, err := ReadMetadata(fmt.Sprintf("%s/%d/metadata.json", seriesIDRootDir, seriesID))
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

// PathExists checks if a file or directory exists at the given path.
func PathExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return err == nil
}

// ReadMetadata reads and parses a metadata.json file.
func ReadMetadata(metadataJsonPath string) (cstructs.MetadataJson, error) {
	var data cstructs.MetadataJson

	if !PathExists(metadataJsonPath) {
		return data, fmt.Errorf("metadata file %s does not exist", metadataJsonPath)
	}

	content, err := os.ReadFile(metadataJsonPath)
	if err != nil {
		return data, fmt.Errorf("read metadata JSON file %s: %w", metadataJsonPath, err)
	}

	err = json.Unmarshal(content, &data)
	if err != nil {
		return data, fmt.Errorf("unmarshal metadata JSON from %s: %w", metadataJsonPath, err)
	}

	return data, nil
}

