package downloader

import (
	"comicrawl/internal/cstructs"
	"comicrawl/internal/util"
	"comicrawl/internal/util/fileio"
	"fmt"
	"io"
	"os"
	"sync"
)

type Downloader struct {
    *cstructs.Downloader
}

func NewCustomDownloader(base_dir string) *Downloader {
    return &Downloader{
        &cstructs.Downloader{
            BaseDownloadDir: base_dir,
        },
    }
}

func (d *Downloader) StartDownload(seriesData cstructs.DownloadSeriesData) {
    var wg sync.WaitGroup
    jobData := d.DownloadBatch

    for _, data := range jobData {
        wg.Add(1)

        go func(ds cstructs.DownloadSeriesData) {
            wg.Done()

            for _, chapter := range ds.ChapterData {
                limit := make(chan struct{}, cstructs.MAX_PAGES)

                var pg sync.WaitGroup
                for _, page := range chapter.ImagesData {
                     pg.Add(1)   
                     limit <- struct{}{}

                     go func(p cstructs.DownloadPageData) {
                         defer pg.Done()
                         defer func() { <- limit }()

                         chapterStr := util.ChapterFloatToString(chapter.ChapterNumber)
                         err := d.SaveImage(p.ImageBody, ds.SeriesID, ds.SourceProvider, chapterStr, p.PageNumber, p.ImageExt)
                         if err != nil {
                             fmt.Printf("Couldn't download image page | chapter num: %f | Image Number: %d \n", chapter.ChapterNumber, p.PageNumber)
                         }
                     }(page)
                }
                pg.Wait()
            }
        }(data)
    }

    wg.Wait()
    fmt.Println("Done!!!")
}

func (d *Downloader) AddToQueue(seriesData cstructs.DownloadSeriesData) {
    d.QueueBatch = append(d.QueueBatch, seriesData)
}

func (d *Downloader) UpdateDownloadBatch() bool {
    if len(d.QueueBatch) == 0 {
        return false
    }

    d.DownloadBatch = [cstructs.MAX_SERIES]cstructs.DownloadSeriesData{}
    count := min(len(d.QueueBatch), cstructs.MAX_SERIES)
    copy(d.DownloadBatch[:], d.QueueBatch[:count])
    d.QueueBatch = d.QueueBatch[count:]

    return true
}

// The main image data is the image Response Body
func (d *Downloader) SaveImage(
    imageBody io.Reader,
    seriesID int64,
    sourceProvider string,
    chapterNum string,
    pageNum int,
    imageExt string,
) error {
    filePath := fmt.Sprintf("%s/%d/%s/chap_%s/page_%d%s", d.BaseDownloadDir, seriesID, sourceProvider, chapterNum, pageNum, imageExt)
    if fileio.PathExists(filePath) {
        return fmt.Errorf("File already exist\n")
    }
    out, err := os.Create(filePath)
    if err != nil {
        return err
    }

    defer out.Close()

    _, err = io.Copy(out, imageBody)
    fmt.Printf("Image Saved: %s\n", filePath)
    return nil
}
