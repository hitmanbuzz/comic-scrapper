package scrapper

import (
	"fmt"
	"os"
)

type ScrapeMode string

const (
	ModeFull        ScrapeMode = "full"
	ModeIncremental ScrapeMode = "incremental"
	ModeSingle      ScrapeMode = "single"
)

// Apply the mode for scrapping
func ApplyScrapperMode(modeFlag *string) ScrapeMode {
	var mode ScrapeMode
	
	switch *modeFlag {
	case "full":
		mode = ModeFull
	case "incremental":
		mode = ModeIncremental
	case "single":
		mode = ModeSingle
	default:
		fmt.Fprintf(os.Stderr, "Invalid mode: %s. Must be 'full', 'incremental', or 'single'\n", *modeFlag)
		os.Exit(1)
	}

	return mode
}
