package main

import (
	"comicrawl/internal/config"
	"comicrawl/internal/cstructs/scrape_data"
	"comicrawl/internal/httpclient"
	"comicrawl/internal/mangaupdates"
	"comicrawl/internal/system"
	"comicrawl/internal/util"
	"comicrawl/internal/util/fileio"
	"comicrawl/internal/util/metadata"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

func main() {
    newFlags := system.CreateNewFlags()

	cfg, err := config.LoadConfig(*newFlags.ConfigPath)
	if err != nil {
	    fmt.Printf("Error Reading config file\n")
	    return
	}

	logger := system.SetupLogger(cfg, newFlags)
	logger.UpdateConfigFlags()

	if validationErr := cfg.Validate(); validationErr != nil {
		fmt.Printf("invalid configuration: %v\n", validationErr)
		return
	}

	logger.ConfigLogging()
    
    data, err := collectComicData(cfg.Bucket)
	if err != nil {
		panic(err)
	}

	httpClient, err := httpclient.NewHTTPClient(cfg, logger.Logger, nil)
	if err != nil {
		fmt.Printf("failed to create HTTP client: %v", err)
        fmt.Printf("Error: %v\n", err)
		return
	}

	// k = series id
	for k, v := range data {
        seriesInfo, err := mangaupdates.GetSeriesInfo(context.TODO(), util.StringToInt64(k), httpClient)
        if err != nil {
            fmt.Printf("Failed to get series info | series id: %s\n", k)
            fmt.Printf("Error: %v\n", err)
            continue
        }
	    
        var mData scrape_data.MetadataJson
        mData.Title = seriesInfo.Title
        mData.Description = seriesInfo.Description
        mData.ComicStatus = extractStatus(seriesInfo.ComicStatus)
        mData.ComicType = seriesInfo.ComicType
        mData.MuUrl = seriesInfo.URL
        mData.MuSeriesId = seriesInfo.SeriesId
        mData.ReleaseYear = seriesInfo.ReleaseYear

        // Alt-Titles
        for _, at := range seriesInfo.AltTitles {
            mData.AltTitles = append(mData.AltTitles, at.Title)
        }

        // Authors
        for _, au := range seriesInfo.Authors {
            mData.Authors = append(mData.Authors, au.AuthorName)
        }

        // Genres
        for _, gn := range seriesInfo.Genres {
            mData.Genres = append(mData.Genres, gn.Genre)
        }

        // Categories
        for _, cat := range seriesInfo.Categories {
        	mData.Categories = append(mData.Categories, cat.Category)
        }
	    
        // Path where to store the series thumbnail image
        thumbnailDirPath := fmt.Sprintf("%s/%s/thumbnail", cfg.Bucket, k)
        err = os.MkdirAll(thumbnailDirPath, 0755)
        if err != nil {
            fmt.Printf("failed to create thumbnail dir | series id : %s\n", k)
            fmt.Printf("Error: %v\n", err)
            continue
        }

        var imgFullPath string

        // imgFile = the image file name (eg: thumbnail.jpg)
        imgFile := fmt.Sprintf("thumbnail%s", filepath.Ext(seriesInfo.Image.URL.Thumbnail))
        if !util.IsPathExists(fmt.Sprintf("%s/%s", thumbnailDirPath, imgFile)) {
            err, fullPath := fileio.DownloadImage(context.TODO(), httpClient, seriesInfo.Image.URL.Thumbnail, thumbnailDirPath, imgFile)
            if err != nil {
                fmt.Printf("Couldn't download image | url: %s\n", seriesInfo.Image.URL.Thumbnail)
                fmt.Printf("Error: %v\n", err)
            }

            imgFullPath = fullPath
        }

        mData.Thumbnail = fmt.Sprintf("%s/%s", thumbnailDirPath, imgFile)
        scrapData, err := processSeriesData(v, cfg.Bucket, k)
        if err != nil {
            fmt.Printf("failed to process series data")
            fmt.Printf("Error: %v\n", err)
            continue
        }

        mData.ScrapedData = append(mData.ScrapedData, scrapData)

		file_path := fmt.Sprintf("%s/%s/metadata.json", cfg.Bucket, k)
        err = metadata.GenerateMetadata(mData, file_path)
        if err != nil {
            fmt.Printf("Failed to generate metadata | series id: %s\n", k)
            fmt.Printf("Error: %v\n", err)
            continue
        }

		fmt.Printf("\n[SUCCESSFULLY GENERATED METADATA]\n")
		fmt.Printf("Series Title: %s\n", mData.Title)
		fmt.Printf("Series ID: %d\n", mData.MuSeriesId)
		fmt.Printf("Thumbnail Path: %s\n", imgFullPath)
		fmt.Printf("Metadata Json Path: %s\n", file_path)
		fmt.Printf("\n")
	}
}

func processSeriesData(s SourceData, bucketDir string, seriesId string) (scrape_data.SourceProviderScrapedData, error) {
    var sData scrape_data.SourceProviderScrapedData

    sData.Name = s.SourceName
    sData.TotalChapters = len(s.Chapter)
    sData.SourcePath = fmt.Sprintf("%s/%s/%s", bucketDir, seriesId, s.SourceName)

    if len(s.Chapter) == 0 {
        return sData, fmt.Errorf("no chapter found\n")
    }

    var wg sync.WaitGroup
    var mu sync.Mutex
    
    maxChap := s.Chapter[0].ChapterNumber
    for i := 0; i < len(s.Chapter); i++ {
        wg.Add(1)
        go func(ch ChapterData) {
            defer wg.Done()

            var chpData scrape_data.SourceChapterData 

            for _, img := range ch.Image {
                chpData.ChapterNumber = ch.ChapterNumber
                chpData.ChapterPath = ch.ChapterDir
                chpData.ChapterImages = append(chpData.ChapterImages, img.ImagePath)
            }
            
            mu.Lock()
            sData.ChapterData = append(sData.ChapterData, chpData)
            
            if ch.ChapterNumber > maxChap {
                maxChap = s.Chapter[i].ChapterNumber
            }
            mu.Unlock()
                        
        }(s.Chapter[i])
    }

    wg.Wait()
    sData.LatestChapter = maxChap

    return sData, nil
}

type ImageData struct {
    ImageNumber int
    // full image path
    ImagePath string
}

type ChapterData struct {
    ChapterNumber float32
    // Path to chapter
    ChapterDir string
    Image []ImageData
}

type SourceData struct {
    // Scanlator name
    SourceName string
    Chapter []ChapterData
}

// eg: chap_12_5 = 12.5
func parseChapterNumber(name string) (float32, bool) {
	if !strings.HasPrefix(name, "chap_") {
		return 0, false
	}

	num := strings.TrimPrefix(name, "chap_")
	num = strings.Replace(num, "_", ".", 1)

	f := util.StringToFloat(num)
	if f == -69 {
		return -69, false
	}
	
	return f, true
}

// eg: img_14.webp = 14
func parseImageNumber(name string) (int, bool) {
	if !strings.HasPrefix(name, "img_") {
		return 0, false
	}

	before, _, ok := strings.Cut(name, ".")
	if !ok {
		return 0, false
	}

	num := strings.TrimPrefix(before, "img_")
	i := util.StringToInt64(num)
	if i == -69 {
		return -69, false
	}
	
	return int(i), true
}

func extractStatus(s string) string {
	s = strings.ToLower(s)

	switch {
	case strings.Contains(s, "hiatus"):
		return "hiatus"
	case strings.Contains(s, "completed"):
		return "completed"
	case strings.Contains(s, "ongoing"):
		return "ongoing"
	case strings.Contains(s, "finished"):
		return "finished"
	default:
		return "ongoing"
	}
}

func collectComicData(root string) (map[string]SourceData, error) {
	result := make(map[string]SourceData)

	level1, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	for _, d1 := range level1 {
		if !d1.IsDir() {
			continue
		}

		numberKey := d1.Name()
		numberPath := filepath.Join(root, numberKey)

		// depth 2: source directories (e.g. asura)
		level2, err := os.ReadDir(numberPath)
		if err != nil {
			continue
		}

		for _, d2 := range level2 {
			if !d2.IsDir() || d2.Name() == "thumbnail" {
				continue
			}

			source := SourceData{
				SourceName: d2.Name(),
			}

			sourcePath := filepath.Join(numberPath, d2.Name())

			// depth 3: chapter directories
			level3, err := os.ReadDir(sourcePath)
			if err != nil {
				continue
			}

			for _, d3 := range level3 {
				if !d3.IsDir() {
					continue
				}

				chNum, ok := parseChapterNumber(d3.Name())
				if !ok {
					continue
				}

				chapterPath := filepath.Join(sourcePath, d3.Name())

				chapter := ChapterData{
					ChapterNumber: chNum,
					ChapterDir:    chapterPath,
				}

				// depth 4: images
				files, err := os.ReadDir(chapterPath)
				if err != nil {
					continue
				}

				for _, f := range files {
					if f.IsDir() {
						continue
					}

					imgNum, ok := parseImageNumber(f.Name())
					if !ok {
						continue
					}

					img := ImageData{
						ImageNumber: imgNum,
						ImagePath:   filepath.Join(chapterPath, f.Name()),
					}

					chapter.Image = append(chapter.Image, img)
				}

				source.Chapter = append(source.Chapter, chapter)
			}

			result[numberKey] = source
		}
	}

	return result, nil
}
