package system

import "flag"

// Local Flag configs field
type LogFlagConfig struct {
	ConfigPath *string
	ModeFlag   *string

	// CLI override flags
	SourcesFlag       *string
	IncludeSeriesFlag *string
	ExcludeSeriesFlag *string
	LimitSeriesFlag   *int
	LimitChaptersFlag *int
	DryRunFlag        *bool
}

// Add new flags here (Update the above struct and return type struct field also)
func CreateNewFlags() *LogFlagConfig {
	configPath := flag.String("config", "config.yaml", "Path to config file")
	modeFlag := flag.String("mode", "full", "Scraping mode: full, incremental, or single")

	// CLI override flags
	sourcesFlag := flag.String("sources", "", "Comma-separated list of sources to include (e.g., 'asurascans,webtoon')")
	includeSeriesFlag := flag.String("include-series", "", "Comma-separated list of series to include")
	excludeSeriesFlag := flag.String("exclude-series", "", "Comma-separated list of series to exclude")
	limitSeriesFlag := flag.Int("limit-series", 0, "Limit number of series to process (0 = no limit)")
	limitChaptersFlag := flag.Int("limit-chapters", 0, "Limit number of chapters per series (0 = no limit)")
	dryRunFlag := flag.Bool("dry-run", false, "Perform a dry run without downloading")

	flag.Parse()

	return &LogFlagConfig{
		ConfigPath:        configPath,
		ModeFlag:          modeFlag,
		SourcesFlag:       sourcesFlag,
		IncludeSeriesFlag: includeSeriesFlag,
		ExcludeSeriesFlag: excludeSeriesFlag,
		LimitSeriesFlag:   limitSeriesFlag,
		LimitChaptersFlag: limitChaptersFlag,
		DryRunFlag:        dryRunFlag,
	}
}
