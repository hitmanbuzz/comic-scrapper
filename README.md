# Comic Crawl

Scrape comics from scanlation sites.

### STATUS: NOT TESTED (WILL HAVE ERRORS)

### Prerequisites:
- Go 1.24+
- [aria2](https://github.com/aria2/aria2) RPC server
- Docker (optional, for Cloudflare bypass)

### Quick Start

1. **Clone and build:**
```bash
git clone <repository-url>
cd comicrawl
go build -o comicrawl ./cmd/app/main.go
```

2. **Run the application:**
```bash
# Start aria2c RPC server (required)
chmod +x aria-srv.sh
./aria-srv.sh &

# Run the main application
./comicrawl
# or
go run ./cmd/app/main.go
```

### Individual Components

#### 1. Fetch series from scanlator sites:
```bash
go run cmd/series/series.go
```

#### 2. Filter series using MangaUpdates data:
```bash
go run cmd/filter/filter.go
```

#### 3. Run integration tests:
```bash
go run cmd/test/main.go
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
