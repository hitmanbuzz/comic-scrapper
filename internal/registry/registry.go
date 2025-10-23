package registry

import (
    "comicrawl/internal/sources"
    "comicrawl/internal/sources/scanlators"
    "log/slog"
)

// AddSources returns all available source implementations
func AddSources(logger *slog.Logger) []sources.Source {
    return []sources.Source{
        scanlators.NewAsuraScans(logger),
        scanlators.NewWebtoon(logger),
        // scanlators.NewUtoon(logger), // Will add after I finished it
        scanlators.NewFlameComics(logger),
    }
}
