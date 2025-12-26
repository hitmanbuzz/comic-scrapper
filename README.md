# Comic Crawl

###### Scrape comics from scanlation sites.

### STATUS: *TESTED (NEED TO TEST WITH OTHER SOURCES)*

### Prerequisites:
- Makefile
- Go Version >= 1.24
- Docker (optional, for Cloudflare bypass)

### Working Sources:
     Asurascans
     Utoon
     Madarascans

### Individual Components:
> Run from step 1 to last (if wanted to run the whole project)

#### 1. Fetch series from scanlator sites:
```bash
make series
```

#### 2. Filter series using MangaUpdates data:
```bash
make filter
```

#### 3. Fetch All Series Chapter & Images URL (From Scanlator)
```bash
make scrape
```

#### 4. Download All series in proper format (need to have Series Chapter & Image URL)
```bash
make download
```

#### 5. Generate Metadata for download series
```bash
make metadata
```

### Development

#### Install tools:
```bash
make install-tools
```

#### Code quality checks:
```bash
make fmt      # Format code
make lint     # Run linters
make test     # Run tests
make all      # Format, lint, and test
```

#### Pre-commit hook:
```bash
make pre-commit
```

### Optional: Cloudflare Bypass

For sites protected by Cloudflare:
```bash
docker-compose up
```
