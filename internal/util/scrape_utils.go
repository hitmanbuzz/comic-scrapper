package util

import (
	"comicrawl/internal/cstructs"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Use to save series from scanlators in a json file (eg: asura_series.json, webtoon_series.json)
// NOTE: Some Refactor can be done but for it works (shit code here made by hitman)
func WriteSourceSeriesJson(full_series cstructs.FullSeriesResponse) {
	jsonData, err := json.MarshalIndent(full_series, "", "  ")
	if err != nil {
		log.Fatal(err)
	}

	dirPath := "series_data"

	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		err := os.Mkdir(dirPath, 0755)
		if err != nil {
			fmt.Println("Error creating directory:", err)
			return
		}
		fmt.Println("Directory created:", dirPath)
	}
	
	fileName := full_series.GroupName
	fileName = strings.ToLower(fileName)
	fileName = strings.ReplaceAll(fileName, "-", "_")
	fileName = strings.TrimSpace(fileName)
	filePath := fmt.Sprintf("%s/%s_series.json", dirPath, fileName)

	// Move the existing to a backup directory with a timestamp in the name
	if FileExists(filePath) {
		backupDir := "backup_data"

	    // Create backup directory if it doesn't exist
	    err := os.MkdirAll(backupDir, 0755)
	    if err != nil {
	        log.Fatal(err)
	    }

	    // Generate backup file path with timestamp to avoid collisions
	    timestamp := time.Now().Format("20060102_150405")
	    fileName := filepath.Base(filePath)
	    backupPath := filepath.Join(backupDir, fmt.Sprintf("%s.%s", timestamp, fileName))

	    // Move existing file to backup directory
	    err = os.Rename(filePath, backupPath)
	    if err != nil {
	        log.Fatal(err)
	    }

	    fmt.Printf("[MOVED] Existing file moved to: %s\n", backupPath)
	}

	err = os.WriteFile(filePath, jsonData, 0644)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("[DONE] Source Scrapping | File: %s\n", filePath)
}

// Read the json data from respective source provider series json file
//
// eg: asura_series.json, utoon_series.json, etc
func ReadSourceSeriesJson(jsonFile string) cstructs.FullSeriesResponse {
	var sourceSeries cstructs.FullSeriesResponse

	if !FileExists(jsonFile) {
		fmt.Printf("%s doesn't exist\n", jsonFile)
		return sourceSeries
	}

	content, err := os.ReadFile(jsonFile)
	if err != nil {
		fmt.Printf("Could Read Json File | File: %s\n", jsonFile)
		return sourceSeries
	}
	
	err = json.Unmarshal(content, &sourceSeries)
	if err != nil {
		fmt.Printf("[ERROR] Couldn't unmarshal\n")
		return sourceSeries
	}

	return sourceSeries
}

// The final proper comic metadata in json file that will be use by KomiKura
func GenerateMetadataJson(data *cstructs.MetadataJson, dir_name string) {
	file_path := fmt.Sprintf("%s/metadata.json", dir_name)

	if FileExists(file_path) {
		fmt.Printf("[SKIPPED] Metadata file already exists: %s\n", file_path)
		return
	}

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Fatal(err)
		return
	}

	err = os.WriteFile(file_path, jsonData, 0644)
	if err != nil {
		log.Fatal(err)
		return
	}

	fmt.Printf("[DONE] Metadata Scrapping | Title: %s\n", data.Title)
}

