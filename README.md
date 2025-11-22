# Comic Crawl

### NOTE:
- Every process like `scrapping` `update` `filter` should have their own separate process (look `cmd/`)

### Todo
- [ ] Connect MU with Source Provider (Need to fix the downloading/scrapping part)
- [ ] Storing those scrapped data in a proper structure
- [ ] Implement New Update Checker for Comics
- [ ] Make a small scrapping testing environment
- [ ] Refactor FlareSolverr and Aria2c Source Code for optimization (optional)
- [ ] Remove unwanted code from the codebase
- [ ] Refactor Source Provider Code if needed (Optional)
- [ ] Make Documentation or Notes to the existing codebase (mostly scrape, scanlator, source, disk)

### Prerequisites:
- [aria2](https://github.com/aria2/aria2)
- Docker

### Run (Each Separate Process)
#### Get All Series from source provider (scanlator)

`go run cmd/series/series.go`

#### Filter those series

`go run cmd/filter/filter.go`

### How to run Project? (NOT WORKING)
> *For now, it only run on Linux*

```
chmod +x aria-srv.sh
./aria-srv.sh
```

Open a new terminal and run this command

```
go run cmd/app/main.go
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
