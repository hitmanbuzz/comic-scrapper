package scraper

import (
	"fmt"
)

type ScrapeMode string

const (
	ModeFull        ScrapeMode = "full"
	ModeIncremental ScrapeMode = "incremental"
	ModeSingle      ScrapeMode = "single"
)

// Parse the scrape mode from flag string
func ParseScrapeMode(modeFlag *string) (ScrapeMode, error) {
	var mode ScrapeMode

	switch *modeFlag {
	case "full":
		mode = ModeFull
	case "incremental":
		mode = ModeIncremental
	case "single":
		mode = ModeSingle
	default:
		return "", fmt.Errorf("invalid mode: %s. Must be 'full', 'incremental', or 'single'", *modeFlag)
	}

	return mode, nil
}
