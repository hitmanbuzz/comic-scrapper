# Scraper

Will contain the code for scrapping part.

Anything related to scrapping from scanlator website should be put here.
The logic part of scrapping can be put here or not depending on how it is dependent.

For example, every scanlator uses different logic to scrap their website. So, we can't put logic for every scanlator here.

Only put the logic that will be use by multiple sources outside of this directory but within the whole project.

### Codebase

We have `scrape_series.go` in this directory.

Task: It scraps the following data from every scanlator
- Scanlator URL
- Scanlator Name
- Total Series (found in scanlator website)
- Total Images (from the scanlator website)
- Series Data - Contain the following data:
	- Series ID (It is fetch from MU by matching the series name from scanlator website)
	- Series Name (Comic Title)
	- Series URL (URL for series from the scanlator website)
	- Total Chapter (Number of chapter the series contain)
	- Total Images (Number of images the series have)
	- ChapterData - Contain the following data in array:
		- Chapter Number
		- Chapter URL (The URL of the chapter for that series in scanlator website)
		- Chapter Name (optional, depend whether the scanlator support chapter title)
		- Total Images (Number of images the current chapter have)
		- Image - Contain the following data in array:
			- Image Number (Need to be in order so not messed up the chapter images)
			- Image URL (Contain the image URL)


All this data will be scrap from each scanlator website using their respective logic code in `internal/sources/scanlator/*`

These data will be stored in a json file with this format: <scanlator_name>_series_data.json
example: `asura_series_data.json`, `utoon_series_data.json`


### Usage:

The data stored in the json file is use by downloader in `internal/downloader/custom_downloader.go`

It will download all the series data and stored it in a directory, the `root directory` path is put in the config file so it depends on where to store it.

The downloaded series is stored in this structure:

```
root_dir/
	series_id/
			scanlator_name/
				chap_1/
					img_0.png
					img_1.png
				chap_2/
					img_0.png
					img_1.png
```
