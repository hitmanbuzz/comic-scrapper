# Comicrawl Usage


## Configuration

Create `config.yaml`:

```yaml
bucket: /tmp/comicrawl  # Base directory for disk storage
storage_type: disk      # Storage type (disk only)
flaresolverr_url: http://localhost:8191/v1
user_agent: "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
requests_per_second: 5
download_workers: 32
scrape_only: ""
log_level: info
request_timeout: 30s
max_retries: 3
```

## Running

```bash
# Build
go build -o comicrawl

# Run with default config
./comicrawl

# Run with custom config
./comicrawl -config /path/to/config.yaml
```

## Features

- **Multiple Sources**: Currently supports AsuraScans
- **Rate Limiting**: Configurable requests per second
- **Disk Storage**: Stores images and metadata in local file system
- **Parallel Downloads**: Concurrent chapter processing
- **Metadata Tracking**: Keeps track of downloaded chapters to avoid duplicates

## Output Structure

Files are stored on disk with this structure:

```
series-slug/
  meta.json           # Series metadata
  chapter-1/
    page-001.webp     # Chapter pages
    page-002.webp
    ...
  chapter-2/
    ...
```