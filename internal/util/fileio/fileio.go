package fileio

import (
	"comicrawl/internal/config"
	"comicrawl/internal/cstructs/download_data"
	"comicrawl/internal/cstructs/scrape_data"
	"comicrawl/internal/httpclient"
	"comicrawl/internal/util"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WriteSourceSeries writes series data from a source provider to a JSON file.
// 
// The file is saved in the series_data directory with the format: <scanlator>_series.json
// 
// If a file with the same name already exists, it's moved to backup_data with a timestamp.
func WriteSourceSeries(fullSeries scrape_data.FullSeriesResponse, cfg *config.Config) error {
	jsonData, err := json.MarshalIndent(fullSeries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal JSON: %w", err)
	}

	dirPath := fmt.Sprintf("%s/%s", cfg.LocalDir, cfg.SeriesListDir)

	if _, statErr := os.Stat(dirPath); os.IsNotExist(statErr) {
		mkdirErr := os.MkdirAll(dirPath, 0755)
		if mkdirErr != nil {
			return fmt.Errorf("create directory %s: %w", dirPath, mkdirErr)
		}
	}

	// Cleaning the fileName
	fileName := fullSeries.GroupName
	fileName = strings.ToLower(fileName)
	fileName = strings.ReplaceAll(fileName, "-", "_")
	fileName = strings.TrimSpace(fileName)
	filePath := fmt.Sprintf("%s/%s_series.json", dirPath, fileName)

	// Move the existing to a backup directory with a timestamp in the name
	if util.IsPathExists(filePath) {
		backupDir := fmt.Sprintf("%s/%s", cfg.LocalDir, cfg.BackupDataDir)

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
// The file should be in the format: <scanlator>_series.json
func ReadSourceSeries(jsonFile string) (scrape_data.FullSeriesResponse, error) {
	var sourceSeries scrape_data.FullSeriesResponse

	if !util.IsPathExists(jsonFile) {
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


// Will create in format: `<scanlator>_series_data.json` based on the data provided on the 1st argument
//
// Need config to know where the base directory to store the json file
func WriteSeriesData(allSeriesData download_data.DownloadData, cfg *config.Config) error {
	jsonData, err := json.MarshalIndent(allSeriesData, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal JSON: %w", err)
	}

	dirPath := fmt.Sprintf("%s/%s", cfg.LocalDir, cfg.SeriesDataDir)

	if _, statErr := os.Stat(dirPath); os.IsNotExist(statErr) {
		mkdirErr := os.MkdirAll(dirPath, 0755)
		if mkdirErr != nil {
			return fmt.Errorf("create directory %s: %w", dirPath, mkdirErr)
		}
	}

	// The full path where it will store `<scanlator>_series_data.json`
	fPath := fmt.Sprintf("%s/%s_series_data.json", dirPath, allSeriesData.ScanName)
	err = os.WriteFile(fPath, jsonData, 0600)
	if err != nil {
		return fmt.Errorf("write file %s: %w", fPath, err)
	}

	fmt.Printf("Successfully Save All Series Data | Source Name: %s | Total Series: %d\n", allSeriesData.ScanName, allSeriesData.TotalSeries)

	return nil
}


// Will read `<scanlator>_series_data.json` based on the full path of the series data json file as an argument
func ReadSeriesData(jsonFile string) (download_data.DownloadData, error) {
	var sourceDataSeries download_data.DownloadData

	if !util.IsPathExists(jsonFile) {
		return sourceDataSeries, fmt.Errorf("file %s does not exist", jsonFile)
	}

	content, err := os.ReadFile(jsonFile)
	if err != nil {
		return sourceDataSeries, fmt.Errorf("read JSON file %s: %w", jsonFile, err)
	}

	err = json.Unmarshal(content, &sourceDataSeries)
	if err != nil {
		return sourceDataSeries, fmt.Errorf("unmarshal JSON from %s: %w", jsonFile, err)
	}

	return sourceDataSeries, nil
}

// Use for downlading images for certain logics
//
// Parameter:
//
// url = The URL for the image that is going to be download
//
// dirPath = The directory path where the image will be downloaded
//
// fileName = The image file name (will download the image in this name)
//
// Return Error and full path of the image
func DownloadImage(ctx context.Context, client *httpclient.HTTPClient, url string, dirPath string, fileName string) error {
	// The Full Path of where the image will be downloaded
	fullPath := fmt.Sprintf("%s/%s", dirPath, fileName)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %d\n", resp.StatusCode)
	}

	err = os.MkdirAll(dirPath, 0755)
	if err != nil {
		return err
	}
	
	file, err := os.Create(fullPath)
	if err != nil {
		return err
	}

	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	return err
}

