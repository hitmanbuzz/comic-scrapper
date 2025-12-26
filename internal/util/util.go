package util

import (
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/hbollon/go-edlib"
)

// NOTE: Add any reusable user-defined function that doesn't rely on any part of the scraper codebase

// -1 return means fail in parsing the slug
//
// It is use for conerting MU slug to ID
func ParseSlugToId(slug string) int64 {
	id, err := strconv.ParseInt(slug, 36, 64)
	if err != nil {
		return -1
	}

	return id
}

// Convert an array of slugs to an array of ID
func ParseSlugsToIds(slugs []string) []int64 {
	ids := make([]int64, 0, len(slugs))
	for _, slug := range slugs {
		if id := ParseSlugToId(slug); id != -1 {
			ids = append(ids, id)
		}
	}
	return ids
}

// -69 result mean it failed to parse
func StringToFloat(s string) float64 {
	float64Value, err := strconv.ParseFloat(s, 32)
	if err != nil {
		fmt.Printf("Error parsing string to float | string: %s\n", s)
		return -69
	}

	return float64Value
}

// -69 result mean it failed to parse
func StringToInt64(s string) int64 {
	num, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		logger := slog.Default()
		logger.Warn("error parsing string to int64", "string", s, "error", err)
		return -69
	}

	return num
}

// Convert `io.ReadCloser` to `string`
func BodyToString(body io.ReadCloser) (string, error) {
	b, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}

	return string(b), nil
}

// Check if directory or file exist
func IsPathExists(file_path string) bool {
	_, err := os.Stat(file_path)
	return err == nil
}

func ChapterFloatToString(chapterNum float64) string {
	if chapterNum == float64(int64(chapterNum)) {
		return strconv.FormatInt(int64(chapterNum), 10)
	}

	s := fmt.Sprintf("%.1f", chapterNum)

	return strings.Replace(s, ".", "_", 1)
}

// NormalizeTitle preprocesses comic titles for better matching by removing punctuation,
// articles, and normalizing whitespace. This handles common variations in title formatting.
func NormalizeTitle(title string) string {
	normalized := strings.ToLower(title)

	for _, article := range []string{"the ", "a ", "an "} {
		if after, ok :=strings.CutPrefix(normalized, article); ok  {
			normalized = after
			break
		}
	}

	normalized = strings.ReplaceAll(normalized, "!", "")
	normalized = strings.ReplaceAll(normalized, "?", "")
	normalized = strings.ReplaceAll(normalized, ":", "")
	normalized = strings.ReplaceAll(normalized, ".", "")
	normalized = strings.ReplaceAll(normalized, ",", "")
	normalized = strings.ReplaceAll(normalized, "'", "")
	normalized = strings.ReplaceAll(normalized, "\"", "")
	normalized = strings.ReplaceAll(normalized, "&", "and")
	normalized = strings.ReplaceAll(normalized, "-", " ")
	normalized = strings.ReplaceAll(normalized, "_", " ")

	normalized = strings.TrimSuffix(normalized, "...")
	normalized = strings.TrimSuffix(normalized, "…")

	normalized = regexp.MustCompile(`\s+`).ReplaceAllString(normalized, " ")
	normalized = strings.TrimSpace(normalized)

	return normalized
}

// IsComicTitleMatch uses multiple string similarity strategies to determine if two comic titles match.
// This multi-strategy approach handles: exact matches, truncation, punctuation differences,
// word reordering, and fuzzy matching. Returns true if match found, similarity score, and any error.
func IsComicTitleMatch(title1, title2 string) (bool, float32, error) {
	norm1 := NormalizeTitle(title1)
	norm2 := NormalizeTitle(title2)

	if norm1 == norm2 {
		return true, 1.0, nil
	}

	clean1 := strings.TrimSuffix(norm1, "")
	clean2 := strings.TrimSuffix(norm2, "")

	if strings.HasPrefix(norm1, clean2) || strings.HasPrefix(norm2, clean1) {
		minLen := min(len(clean1), len(clean2))
		maxLen := max(len(norm1), len(norm2))
		if float32(minLen)/float32(maxLen) >= 0.8 {
			return true, 0.95, nil
		}
	}

	diceScore := SorensenDiceCoefficient(norm1, norm2)
	if diceScore >= 0.75 {
		return true, diceScore, nil
	}

	tokenScore := TokenCosineSimilarity(norm1, norm2)
	if tokenScore >= 0.85 {
		return true, tokenScore, nil
	}

	jaroScore, err := edlib.StringsSimilarity(norm1, norm2, edlib.JaroWinkler)
	if err != nil {
		return false, 0, err
	}

	return jaroScore >= 0.88, jaroScore, nil
}

// SorensenDiceCoefficient calculates similarity based on bigram (2-character pair) overlap.
// This is more forgiving than Levenshtein for truncation and handles character-level differences well.
// Returns a score from 0.0 (no similarity) to 1.0 (identical).
func SorensenDiceCoefficient(s1, s2 string) float32 {
	if len(s1) == 0 || len(s2) == 0 {
		return 0
	}

	if len(s1) < 2 || len(s2) < 2 {
		if s1 == s2 {
			return 1.0
		}
		return 0
	}

	bigrams1 := getBigrams(s1)
	bigrams2 := getBigrams(s2)

	if len(bigrams1) == 0 || len(bigrams2) == 0 {
		return 0
	}

	intersection := 0
	for bigram := range bigrams1 {
		if bigrams2[bigram] {
			intersection++
		}
	}

	// Sørensen-Dice formula: 2 * |intersection| / (|set1| + |set2|)
	return 2.0 * float32(intersection) / float32(len(bigrams1)+len(bigrams2))
}

// extracts all 2-character pairs from a string
func getBigrams(s string) map[string]bool {
	bigrams := make(map[string]bool)
	for i := 0; i < len(s)-1; i++ {
		bigrams[s[i:i+2]] = true
	}
	return bigrams
}

// TokenCosineSimilarity calculates similarity based on word tokens using cosine similarity.
// This treats titles as vectors of words and measures the angle between them.
// Excellent for handling word order differences and partial title matches.
func TokenCosineSimilarity(s1, s2 string) float32 {
	tokens1 := strings.Fields(s1)
	tokens2 := strings.Fields(s2)

	if len(tokens1) == 0 || len(tokens2) == 0 {
		return 0
	}

	// Build token frequency maps
	freq1 := make(map[string]int)
	freq2 := make(map[string]int)

	for _, token := range tokens1 {
		freq1[token]++
	}
	for _, token := range tokens2 {
		freq2[token]++
	}

	// Calculate dot product and magnitudes for cosine similarity
	var dotProduct, mag1, mag2 float32

	// Get all unique tokens from both strings
	allTokens := make(map[string]bool)
	for token := range freq1 {
		allTokens[token] = true
	}
	for token := range freq2 {
		allTokens[token] = true
	}

	// Calculate vector dot product and magnitudes
	for token := range allTokens {
		f1 := float32(freq1[token])
		f2 := float32(freq2[token])

		dotProduct += f1 * f2
		mag1 += f1 * f1
		mag2 += f2 * f2
	}

	if mag1 == 0 || mag2 == 0 {
		return 0
	}

	// Cosine similarity = dot product / (||v1|| * ||v2||)
	return dotProduct / (float32(math.Sqrt(float64(mag1))) * float32(math.Sqrt(float64(mag2))))
}
