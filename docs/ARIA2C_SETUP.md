# Aria2c Setup for Comicrawl

## Overview

Comicrawl now supports using [aria2c](https://aria2.github.io/) for high-performance parallel downloads. Aria2c is a lightweight, multi-protocol, multi-source command-line download utility that can significantly improve download speeds.

## Installation

### Ubuntu/Debian
```bash
sudo apt update
sudo apt install aria2
```

### macOS
```bash
brew install aria2
```

### Windows
Download from: https://github.com/aria2/aria2/releases

## Configuration

### Starting aria2c RPC Server

Start aria2c with RPC enabled:

```bash
aria2c --enable-rpc --rpc-listen-all=false --rpc-listen-port=6800 --max-connection-per-server=16 --split=16 --min-split-size=1M --max-concurrent-downloads=200 --max-overall-download-limit=0 --daemon=true
```

Or create a configuration file `aria2.conf`:

```ini
# Basic Options
dir=/tmp/aria2-downloads
max-connection-per-server=16
split=16
min-split-size=1M
max-concurrent-downloads=200
max-overall-download-limit=0

# RPC Configuration
enable-rpc=true
rpc-listen-all=false
rpc-listen-port=6800
rpc-allow-origin-all=true
rpc-secret=your_secret_here

# Advanced Options
file-allocation=prealloc
disk-cache=32M
continue=true

# Timeouts
timeout=30
retry-wait=2
max-tries=5
```

Then start with:
```bash
aria2c --conf-path=aria2.conf --daemon=true
```

### Comicrawl Configuration

Update your `config.yaml` to enable aria2c:

```yaml
# Aria2c configuration (for high-performance downloads)
aria2c_url: "http://localhost:6800/jsonrpc"
use_aria2c: true

# Optional: If using RPC secret
# aria2c_secret: "your_secret_here"

# Worker configuration (controls how many downloads can be queued)
download_workers: 200
```

## Performance Benefits

- **10-100x faster downloads**: Aria2c can download multiple parts of a file simultaneously
- **Better connection management**: Built-in retry logic and connection pooling
- **Bandwidth optimization**: Intelligent splitting and merging of downloads
- **Resource efficient**: Lower CPU usage compared to custom Go HTTP clients

## Troubleshooting

### Aria2c Not Starting

Check if aria2c is running:
```bash
pgrep aria2c
```

Start aria2c manually:
```bash
aria2c --enable-rpc --rpc-listen-port=6800 --daemon=true
```

### Connection Refused

Ensure the RPC server is running on the correct port:
```bash
netstat -tlnp | grep 6800
```

### Permission Issues

Make sure aria2c has write access to the download directory:
```bash
chmod 755 /tmp/aria2-downloads
```

## Monitoring

You can monitor aria2c downloads using web interfaces like:
- [Aria2 WebUI](https://github.com/ziahamza/webui-aria2)
- [AriaNg](https://github.com/mayswind/AriaNg)

Or use the command line:
```bash
aria2c -s 1
```

## Fallback Behavior

If aria2c is not available, Comicrawl will automatically fall back to the regular HTTP downloader, so your scraping will continue uninterrupted.