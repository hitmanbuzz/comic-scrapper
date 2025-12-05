package fileio

import (
	"comicrawl/internal/cstructs"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// WriteSourceSeries writes series data from a source provider to a JSON file.
// The file is saved in the series_data directory with the format: {groupname}_series.json
// If a file with the same name already exists, it's moved to backup_data with a timestamp.
func WriteSourceSeries(fullSeries cstructs.FullSeriesResponse) error {
	jsonData, err := json.MarshalIndent(fullSeries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal JSON: %w", err)
	}

	dirPath := "series_data"

	if _, statErr := os.Stat(dirPath); os.IsNotExist(statErr) {
		mkdirErr := os.Mkdir(dirPath, 0755)
		if mkdirErr != nil {
			return fmt.Errorf("create directory %s: %w", dirPath, mkdirErr)
		}
	}

	fileName := fullSeries.GroupName
	fileName = strings.ToLower(fileName)
	fileName = strings.ReplaceAll(fileName, "-", "_")
	fileName = strings.TrimSpace(fileName)
	filePath := fmt.Sprintf("%s/%s_series.json", dirPath, fileName)

	// Move the existing to a backup directory with a timestamp in the name
	if PathExists(filePath) {
		backupDir := "backup_data"

		// Create backup directory if it doesn't exist
		backupErr := os.MkdirAll(backupDir, 0755)
		if backupErr != nil {
			return fmt.Errorf("create backup directory %s: %w", backupDir, backupErr)
		}

		// Generate backup file path with timestamp to avoid collisions
		timestamp := time.Now().Format("20060102_150405")
		baseName := filepath.Base(filePath)
		backupPath := filepath.Join(backupDir, fmt.Sprintf("%s.%s", timestamp, baseName))

		// Move existing file to backup directory
		err = os.Rename(filePath, backupPath)
		if err != nil {
			return fmt.Errorf("move %s to %s: %w", baseName, backupPath, err)
		}

		logger := slog.Default()
		logger.Info("moved existing file to backup", "backup_path", backupPath, "original_path", filePath)
	}

	err = os.WriteFile(filePath, jsonData, 0600)
	if err != nil {
		return fmt.Errorf("write file %s: %w", filePath, err)
	}

	logger := slog.Default()
	logger.Info("source scraping completed", "file", filePath, "group", fullSeries.GroupName, "series_count", fullSeries.TotalSeries)
	return nil
}

// ReadSourceSeries reads series data from a source provider JSON file.
// The file should be in the format: {groupname}_series.json
func ReadSourceSeries(jsonFile string) (cstructs.FullSeriesResponse, error) {
	var sourceSeries cstructs.FullSeriesResponse

	if !PathExists(jsonFile) {
		return sourceSeries, fmt.Errorf("file %s does not exist", jsonFile)
	}

	content, err := os.ReadFile(jsonFile)
	if err != nil {
		return sourceSeries, fmt.Errorf("read JSON file %s: %w", jsonFile, err)
	}

	err = json.Unmarshal(content, &sourceSeries)
	if err != nil {
		return sourceSeries, fmt.Errorf("unmarshal JSON from %s: %w", jsonFile, err)
	}

	return sourceSeries, nil
}

// PathExists checks if a file or directory exists at the given path.
func PathExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return err == nil
}

// LoadSourceAllSeries loads all source provider series JSON files from a directory.
// It reads all files matching the pattern *_series.json in the given directory.
func LoadSourceAllSeries(sourceSeriesDir string) ([]cstructs.FullSeriesResponse, error) {
	var jsonFiles []cstructs.FullSeriesResponse
	entries, err := os.ReadDir(sourceSeriesDir)
	if err != nil {
		return jsonFiles, fmt.Errorf("read directory %s: %w", sourceSeriesDir, err)
	}

	for _, e := range entries {
		_, isSeries := IsSourceSeriesJsonFile(e.Name())
		if !e.IsDir() && isSeries {
			sourceData, err := ReadSourceSeries(fmt.Sprintf("%s/%s", sourceSeriesDir, e.Name()))
			if err != nil {
				continue
			}

			jsonFiles = append(jsonFiles, sourceData)
		}
	}

	return jsonFiles, nil
}

// LoadSourcesName loads all source provider names from their series JSON files.
// Returns a list of source names (e.g., ["asura", "webtoon"]).
func LoadSourcesName(sourceSeriesDir string) ([]string, error) {
	var seriesName []string
	entries, err := os.ReadDir(sourceSeriesDir)
	if err != nil {
		return seriesName, fmt.Errorf("read directory %s: %w", sourceSeriesDir, err)
	}

	for _, e := range entries {
		name, isSeries := IsSourceSeriesJsonFile(e.Name())
		if !e.IsDir() && isSeries {
			seriesName = append(seriesName, strings.TrimSpace(name))
		}
	}

	return seriesName, nil
}

// IsSourceSeriesJsonFile checks if a filename matches the pattern for source series JSON files.
// Pattern: {name}_series.json
// Returns the series name and true if it matches the pattern.
func IsSourceSeriesJsonFile(filename string) (string, bool) {
	fileName := filepath.Base(filename)
	pattern := `^(.+)_series\.json$`
	re := regexp.MustCompile(pattern)

	matches := re.FindStringSubmatch(fileName)
	if matches != nil && len(matches) > 1 {
		return matches[1], true
	}

	return "", false
}

// LoadAllSeriesID loads all series IDs from the scraped data directory.
// It reads all directory names that can be parsed as int64 from the given root directory.
func LoadAllSeriesID(seriesIdRootDir string) ([]int64, error) {
	var series []int64

	entries, err := os.ReadDir(seriesIdRootDir)
	if err != nil {
		return series, fmt.Errorf("read directory %s: %w", seriesIdRootDir, err)
	}

	for _, e := range entries {
		seriesNum := stringToInt64(e.Name())

		if e.IsDir() && seriesNum != -69 {
			series = append(series, seriesNum)
		}
	}

	return series, nil
}

// stringToInt64 converts a string to int64, returning -69 on failure.
func stringToInt64(s string) int64 {
	num, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		logger := slog.Default()
		logger.Warn("error parsing string to int64", "string", s, "error", err)
		return -69
	}

	return num
}

