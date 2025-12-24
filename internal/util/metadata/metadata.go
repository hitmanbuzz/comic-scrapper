package metadata

import (
	"comicrawl/internal/cstructs/scrape_data"
	"comicrawl/internal/util"
	"encoding/json"
	"fmt"
	"os"
)

// GenerateMetadata creates or updates a metadata.json file for a series.
// If metadata already exists, it merges new data with existing data.
func GenerateMetadata(data scrape_data.MetadataJson, seriesIdRootDir string, seriesId string) error {
	file_path := fmt.Sprintf("%s/%s/metadata.json", seriesIdRootDir, seriesId)

	finalData := data

	// If metadata already exists, merge with existing data
	if util.IsPathExists(file_path) {
		existingData, err := ReadMetadata(file_path)
		if err != nil {
			return fmt.Errorf("fetch metadata from %s: %w", file_path, err)
		}

		finalData = existingData

		for _, newSourceProvider := range data.ScrapedData {
			// Check if this SourceProvider already exists
			found := false
			for i, existingSourceProvider := range finalData.ScrapedData {
				if existingSourceProvider.Name == newSourceProvider.Name {
					// SourceProvider exists - update it
					found = true

					// Update TotalChapters
					finalData.ScrapedData[i].TotalChapters = newSourceProvider.TotalChapters

					// Update LatestChapter
					finalData.ScrapedData[i].LatestChapter = newSourceProvider.LatestChapter

					// Merge ChapterData (only add new chapters, no duplicates)
					existingChapterNumbers := make(map[float32]bool)
					for _, existingChapter := range existingSourceProvider.ChapterData {
						existingChapterNumbers[existingChapter.ChapterNumber] = true
					}

					// Add only new chapters that don't exist
					for _, newChapter := range newSourceProvider.ChapterData {
						if !existingChapterNumbers[newChapter.ChapterNumber] {
							finalData.ScrapedData[i].ChapterData = append(finalData.ScrapedData[i].ChapterData, newChapter)
						}
					}

					break
				}
			}

			// If SourceProvider doesn't exist, add it as new
			if !found {
				finalData.ScrapedData = append(finalData.ScrapedData, newSourceProvider)
			}
		}
	}

	jsonData, err := json.MarshalIndent(finalData, "", "  ")

	if err != nil {
		return fmt.Errorf("marshal metadata JSON for %s: %w", file_path, err)
	}

	err = os.WriteFile(file_path, jsonData, 0600)
	if err != nil {
		return fmt.Errorf("write metadata JSON to %s: %w", file_path, err)
	}

	fmt.Printf("\n[SUCCESS METADATE GENERATED]\n")
	fmt.Printf("Series Title: %s\n", data.Title)
	fmt.Printf("Series ID: %s\n", seriesId)
	fmt.Printf("Metadata Json Path: %s\n", file_path)
	fmt.Printf("\n")
	return nil
}

// ReadMetadata reads and parses a metadata.json file.
func ReadMetadata(metadataJsonPath string) (scrape_data.MetadataJson, error) {
	var data scrape_data.MetadataJson

	if !util.IsPathExists(metadataJsonPath) {
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
