package util

import (
	"comicrawl/internal/cstructs"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// NOTE:
// `seriesIDRootDir` is the path where all our actual series is downloaded
// and stored in their respective MU series ID folder (and inside each series ID folder
// it contain `metadata.json` and scanlator comic data) 


// Use to save series from scanlators in a json file (eg: asura_series.json, webtoon_series.json)
// NOTE: Some Refactor can be done but it works for now (shit code here made by hitman)
func WriteSourceSeriesJson(full_series cstructs.FullSeriesResponse) error {
	jsonData, err := json.MarshalIndent(full_series, "", "  ")
	if err != nil {
		return fmt.Errorf("[ERROR] Couldn't Json Marshal")
	}

	dirPath := "series_data"

	if _, statErr := os.Stat(dirPath); os.IsNotExist(statErr) {
		mkdirErr := os.Mkdir(dirPath, 0755)
		if mkdirErr != nil {
			// Exiting the program is good since we can't do nothing if the directory can't be created
			return fmt.Errorf("[ERROR] Couldn't create directory: %s\n", mkdirErr)
		}
	}

	fileName := full_series.GroupName
	fileName = strings.ToLower(fileName)
	fileName = strings.ReplaceAll(fileName, "-", "_")
	fileName = strings.TrimSpace(fileName)
	filePath := fmt.Sprintf("%s/%s_series.json", dirPath, fileName)

	// Move the existing to a backup directory with a timestamp in the name
	if !IsPathExists(filePath) {
		backupDir := "backup_data"

		// Create backup directory if it doesn't exist
		backupErr := os.MkdirAll(backupDir, 0755)
		if backupErr != nil {
			// Exiting the program is good since we can't do nothing if the directory can't be created
			return fmt.Errorf("[ERROR] Couldn't create directory: %s\n", backupDir)
		}

		// Generate backup file path with timestamp to avoid collisions
		timestamp := time.Now().Format("20060102_150405")
		fileName := filepath.Base(filePath)
		backupPath := filepath.Join(backupDir, fmt.Sprintf("%s.%s", timestamp, fileName))

		// Move existing file to backup directory
		err = os.Rename(filePath, backupPath)
		if err != nil {
			return fmt.Errorf("[ERROR] Couldn't move %s to %s\n", fileName, backupPath)
		}

		fmt.Printf("[MOVED] Path: %s\n", backupPath)
	}

	err = os.WriteFile(filePath, jsonData, 0600)
	if err != nil {
		return fmt.Errorf("[ERROR] Couldn't Write Data to file: %s\n", filePath)
	}

	fmt.Printf("[DONE] Source Scrapping | File: %s\n", filePath)
	return nil
}

// Read the json data from respective source provider series json file
//
// eg: asura_series.json, utoon_series.json, etc
func ReadSourceSeriesJson(jsonFile string) (cstructs.FullSeriesResponse, error) {
	var sourceSeries cstructs.FullSeriesResponse

	if !IsPathExists(jsonFile) {
		return sourceSeries, fmt.Errorf("[ERROR] %s doesn't exist\n", jsonFile)
	}

	content, err := os.ReadFile(jsonFile)
	if err != nil {
		return sourceSeries, fmt.Errorf("[ERROR] Couldn't Read Json File | Path: %s\n", jsonFile)
	}

	err = json.Unmarshal(content, &sourceSeries)
	if err != nil {
		return sourceSeries, fmt.Errorf("[ERROR] Couldn't Json Unmarshal for Series | Path: %s\n", jsonFile)
	}

	return sourceSeries, nil
}

// The final proper comic metadata in json file that will be use by KomiKura
//
// It will update the existing data if there are new data scrapped
//
// metadata.json
func GenerateMetadataJson(data cstructs.MetadataJson, seriesIdRootDir string, seriesId int64) error {
	file_path := fmt.Sprintf("%s/%d/metadata.json", seriesIdRootDir, seriesId)

	finalData := data

	// If metadata already exists, merge with existing data
	if IsPathExists(file_path) {
		existingData, err := ReadMetadataJson(file_path)
		if err != nil {
			return fmt.Errorf("[ERROR] Couldn't fetch metadata | Path: %s\n", file_path)
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
		return fmt.Errorf("[ERROR] Couldn't Json Marshal for MetadataJson | Path: %s\n", file_path)
	}

	err = os.WriteFile(file_path, jsonData, 0600)
	if err != nil {
		return fmt.Errorf("[ERROR] Couldn't Write Json Data to file: %s\n", file_path)
	}

	fmt.Printf("[DONE] Metadata Scrapping | Title: %s\n", data.Title)
	return nil
}

// Read the metadata json file
//
// Parameter is just the path to the `metadata.json` file
func ReadMetadataJson(metadataJsonPath string) (cstructs.MetadataJson, error) {
	var data cstructs.MetadataJson

	if !IsPathExists(metadataJsonPath) {
		return data, fmt.Errorf("[ERROR] Doesn't exist | Path: %s\n", metadataJsonPath)
	}

	content, err := os.ReadFile(metadataJsonPath)
	if err != nil {
		return data, fmt.Errorf("[ERROR] Couldn't Read Json File | Path: %s\n", metadataJsonPath)
	}

	err = json.Unmarshal(content, &data)
	if err != nil {
		return data, fmt.Errorf("[ERROR] Couldn't Json Unmarshal for Series | Path: %s\n", metadataJsonPath)
	}

	return data, nil
}

// [Optional]
//
// Check if series is already scrapped (like another different scanlator)
func IsSeriesScraped(seriesId int64, seriesIdRootDir string) bool {
	metadataPath := fmt.Sprintf("%s/%d/metadata.json", seriesIdRootDir, seriesId)

	metadata, err := ReadMetadataJson(metadataPath)
	if err != nil {
		return false
	}

	if len(metadata.ScrapedData) > 0 && IsPathExists(metadataPath) {
		return true
	}

	return false
}

// [Optional]
//
// This check if the series is scraped by a specific group
//
// The other one `IsSeriesScrapped` only check if the series is already scraped by any group
func IsSeriesScrapedGroup(seriesId int64, groupName string, seriesIdRootDir string) bool {
	metadataPath := fmt.Sprintf("%s/%d/metadata.json", seriesIdRootDir, seriesId)

	metadata, err := ReadMetadataJson(metadataPath)
	if err != nil {
		return false
	}

	for _, src := range metadata.ScrapedData {
		if src.Name == groupName {
			return true
		}
	}

	return false
}


// [Optional]
//
// Check if the series directory has `metadata.json` or not
func IsMetadataFound(seriesId int64, seriesIdRootDir string) bool {
	mPath := fmt.Sprintf("%s/%d/metadata.json", seriesIdRootDir, seriesId)
	return IsPathExists(mPath)
}

// [Optional]
//
// Check if a chapter from a series is already scrapped by a group or not
func IsChapterScrapped(seriesIDRootDir string, seriesID int64, groupName string, chapterNum float32) bool {
	metadata, err := ReadMetadataJson(fmt.Sprintf("%s/%d/metadata.json", seriesIDRootDir, seriesID))
	if err != nil {
		return false
	}

	if metadata.MuSeriesId != seriesID {
		return false
	}

	for _, data := range metadata.ScrapedData {
		if data.Name == groupName {
			for _, c := range data.ChapterData {
				if c.ChapterNumber == chapterNum {
					return true
				}		
			}
			return false
		}
	}
	
	return false
}

// [Optional]
//
// Get all the series ID (MU Series ID) from the scrapped data directory
func LoadAllSeriesID(seriesIdRootDir string) ([]int64, error) {
	var series []int64
	
	entries, err := os.ReadDir(seriesIdRootDir)
	if err != nil {
		// If we can't read the directory, then simply exiting the program is the best
		return series, fmt.Errorf("Couldn't read directory: %s\n", seriesIdRootDir)
	}

	for _, e := range entries {
		seriesNum := StringToInt64(e.Name())
		
		if e.IsDir() && seriesNum != -69 {
			series = append(series, seriesNum)
		}
	}

	return series, nil
}

// `sourceSeriesDir` is just the root directory name where all scanlator series json file are located
//
// eg: `series_data/utoon_series.json`, here `series_data` is the `sourceSeriesDir` 
func LoadSourceAllSeriesJson(sourceSeriesDir string) ([]cstructs.FullSeriesResponse, error) {
	var jsonFiles []cstructs.FullSeriesResponse
	entries, err := os.ReadDir(sourceSeriesDir)
	if err != nil {
		// If we can't read the directory, then simply exiting the program is the best
		return jsonFiles, fmt.Errorf("Couldn't Read directory: %s\n", sourceSeriesDir)
	}

	for _, e := range entries {
		_, isSeries := IsSourceSeriesJsonFile(e.Name())
		if !e.IsDir() && isSeries {
			sourceData, err := ReadSourceSeriesJson(fmt.Sprintf("%s/%s", sourceSeriesDir, e.Name()))
			if err != nil {
				continue
			}

			jsonFiles = append(jsonFiles, sourceData)
		}
	}

	return jsonFiles, nil
}

// [Optional]
//
// Get all the source provider name by using their series json file
func LoadSourcesName(sourceSeriesDir string) ([]string, error) {
	var seriesName []string
	entries, err := os.ReadDir(sourceSeriesDir)
	if err != nil {
		return seriesName, fmt.Errorf("Couldn't Read directory: %s\n", sourceSeriesDir)
	}

	for _, e := range entries {
		name, isSeries := IsSourceSeriesJsonFile(e.Name())
		if !e.IsDir() && isSeries {
			seriesName = append(seriesName, strings.TrimSpace(name))
		}
	}

	return seriesName, nil
}

// Check if a json file is series source file
//
// Parameter is just the json file path
//
// It return the series name and a bool if it is a series json file
//
// series name = webtoon (removing `_series.json`)
//
// eg: webtoon_series.json = true | webtoon.json = false
func IsSourceSeriesJsonFile(s string) (string, bool) {
	fileName := filepath.Base(s)
	pattern := `^.+_series\.json$`
	re := regexp.MustCompile(pattern)

	matches := re.FindStringSubmatch(fileName)
	if matches != nil {
		return matches[1], true
	}

	return "", false
}
