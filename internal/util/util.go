package util

import (
	"fmt"
	"io"
	"log"
	"os"
	"strconv"

	"github.com/hbollon/go-edlib"
)

// NOTE: Add any reusable user-defined function that doesn't rely on any part of the scrapper codebase

// -1 return means fail in parsing the slug
func ParseSlugToId(slug string) int64 {
	id, err := strconv.ParseInt(slug, 36, 64)
	if err != nil {
		return -1
	}

	return id	
}

// Parse ID back to slug (to be honest not needed but keep it there)
func ParseIdToSlug(id int64) string {
	slug := strconv.FormatInt(id, 36)
	return slug
}

// Convert Response Body to byte
func RespToByte(respBody io.ReadCloser) []byte  {
	result, err := io.ReadAll(respBody)
	if err != nil {
		log.Printf("Error reading response body\n")
		return nil
	}

	return result
}

// -69 result mean it failed to parse
func StringToFloat(s string) float64 {
	float64Value, err := strconv.ParseFloat(s, 32) 
	if err != nil {
		fmt.Println("Error parsing string to float:", err)
		return -69
	}

	return float64Value
}

func FileExists(file_path string) bool {
	_, err := os.Stat(file_path)
	return err == nil
}

func IsSimilarEnough(a, b string, threshold float32) (bool, float32, error) {
	score, err := edlib.StringsSimilarity(a, b, edlib.Levenshtein)
	if err != nil {
		return false, 0, err
	}
	return score >= threshold, score, nil
}
