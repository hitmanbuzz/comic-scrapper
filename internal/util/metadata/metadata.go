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
func GenerateMetadata(data scrape_data.MetadataJson, file_path string) error {
	finalData := data

	// If metadata already exists, merge with existing data
	if util.IsPathExists(file_path) {
		return fmt.Errorf("file already exist")
	}

	jsonData, err := json.MarshalIndent(finalData, "", "  ")

	if err != nil {
		return fmt.Errorf("marshal metadata JSON for %s: %w", file_path, err)
	}

	err = os.WriteFile(file_path, jsonData, 0600)
	if err != nil {
		return fmt.Errorf("write metadata JSON to %s: %w", file_path, err)
	}

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
