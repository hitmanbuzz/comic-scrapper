package scraper

import (
	"comicrawl/internal/config"
	"comicrawl/internal/disk"
	"comicrawl/internal/sources"
	"log/slog"
)

func FilterSources(sourceList []sources.Source, cfg *config.Config) []sources.Source {
	if !cfg.HasSourceFilters() {
		return sourceList
	}

	var filtered []sources.Source
	for _, source := range sourceList {
		if cfg.IsSourceIncluded(source.GetName()) {
			filtered = append(filtered, source)
		}
	}
	return filtered
}

// Define an interface for sources that can compare chapters
type ChapterComparator interface {
	CompareChapters(localChapters []disk.Chapter, remoteChapters []sources.Chapter) (newChapters []sources.Chapter, updatedChapters []sources.Chapter)
}

func FindNewChapters(src sources.Source, localChapters []disk.Chapter, remoteChapters []sources.Chapter, logger *slog.Logger) []sources.Chapter {
	// Try to use CompareChapters if available
	if comparator, ok := src.(ChapterComparator); ok {
		newChapters, _ := comparator.CompareChapters(localChapters, remoteChapters)
		return newChapters
	}

	// Fallback: process all chapters
	logger.Debug("source doesn't implement ChapterComparator, processing all chapters", "source", src.GetName())
	return remoteChapters
}

func ShouldProcessSeries(seriesSlug string, cfg *config.Config) bool {
	return cfg.IsSeriesIncluded(seriesSlug)
}
