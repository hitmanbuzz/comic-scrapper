# Comic Crawl

### Prerequisites:
- [aria2](https://github.com/aria2/aria2)
- Docker

### How to run Project?
> *For now, it only run on Linux*

```
chmod +x aria-srv.sh
./aria-srv.sh
```

Open a new terminal and run this command

```
go run cmd/comicrawl/main.go
```

### Optional: Cloudflare Solver
Open a new terminal and run this command
```
docker-compose up
```

Close Docker
```
docker-compose down
```
