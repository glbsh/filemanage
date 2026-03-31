# Filemanage — File Storage Microservices (Go)

A file storage system built with Go, MinIO, PostgreSQL, and Redis.

## Architecture

```
┌──────────────────┐     ┌──────────────────┐     ┌──────────────────────┐
│  storage-service  │     │ metadata-service  │     │ notification-service  │
│     :8080         │     │     :8081         │     │       :8082           │
│                   │     │                   │     │                       │
│ POST /upload      │     │ POST /metadata    │     │ GET  /events  (SSE)   │
│ GET  /file/{id}   │     │ GET  /metadata/id │     │ GET  /health          │
│ DELETE /file/{id} │     │ GET  /files/      │     └──────────────────────┘
└────────┬──────────┘     └────────┬──────────┘              ▲
         │                         │                          │
         ▼                         ▼                     subscribes
       MinIO                  PostgreSQL                      │
      :9000                    :5432              Redis Pub/Sub :6379
                                                              ▲
                                                    publishes events
                                                  (storage + metadata)
```

### Services

| Service | Port | Responsibility |
|---|---|---|
| storage-service | 8080 | File upload/download/delete via MinIO |
| metadata-service | 8081 | File metadata CRUD via PostgreSQL |
| notification-service | 8082 | Real-time SSE stream via Redis Pub/Sub |

### Infrastructure

| Component | Port | Purpose |
|---|---|---|
| MinIO | 9000 / 9001 | Object storage (S3-compatible) |
| PostgreSQL | 5432 | Metadata database |
| Redis | 6379 | Pub/Sub message broker |

---

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/) and Docker Compose v2
- [Go 1.22+](https://go.dev/dl/) (for running tests or the CLI locally)

---

## Running with Docker Compose

```bash
# Clone the repo
git clone https://github.com/glbsh/filemanage.git
cd filemanage

# Start all services
docker compose up --build

# Run in background
docker compose up --build -d
```

All services will be available once their health checks pass (usually ~15 seconds).

Stop everything:
```bash
docker compose down -v   # -v also removes volumes
```

---

## API Reference

### Storage Service (`:8080`)

#### Upload a file
```bash
curl -X POST http://localhost:8080/upload \
  -F "file=@/path/to/yourfile.txt"
```
Response:
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "filename": "yourfile.txt",
  "size": 1234,
  "content_type": "text/plain",
  "location": "files/550e8400-e29b-41d4-a716-446655440000"
}
```

#### Retrieve a file
```bash
curl -O -J http://localhost:8080/file/{id}
```

#### Delete a file
```bash
curl -X DELETE http://localhost:8080/file/{id}
```

---

### Metadata Service (`:8081`)

#### Save metadata (call after upload)
```bash
curl -X POST http://localhost:8081/metadata \
  -H "Content-Type: application/json" \
  -d '{
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "filename": "yourfile.txt",
    "size": 1234,
    "content_type": "text/plain",
    "location": "files/550e8400-e29b-41d4-a716-446655440000"
  }'
```

#### Get file metadata
```bash
curl http://localhost:8081/metadata/{id}
```

#### List all files
```bash
curl http://localhost:8081/files/
```

---

### Notification Service (`:8082`)

#### Subscribe to real-time events (SSE)
```bash
curl -N http://localhost:8082/events
```

Events are emitted when files are uploaded, deleted, or when metadata is saved:
```
data: {"event":"file.uploaded","id":"550e8400-...","filename":"yourfile.txt","size":1234,"timestamp":"2024-01-01T00:00:00Z"}

data: {"event":"file.deleted","id":"550e8400-...","timestamp":"2024-01-01T00:00:00Z"}

data: {"event":"metadata.created","id":"550e8400-...","filename":"yourfile.txt","timestamp":"2024-01-01T00:00:00Z"}
```

---

## Typical Workflow

```bash
# 1. Upload a file
RESP=$(curl -s -X POST http://localhost:8080/upload -F "file=@README.md")
ID=$(echo $RESP | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")

# 2. Save its metadata
curl -s -X POST http://localhost:8081/metadata \
  -H "Content-Type: application/json" \
  -d "$RESP"

# 3. List all files
curl -s http://localhost:8081/files/ | python3 -m json.tool

# 4. Download the file
curl -O -J http://localhost:8080/file/$ID

# 5. Delete the file
curl -X DELETE http://localhost:8080/file/$ID
```

---

## CLI Tool

A CLI tool is included in the `cli/` directory. See [CLI Usage](#cli-usage) below.

### Build the CLI

```bash
cd cli
go build -o filemanage .
```

### CLI Usage

```bash
# Upload a file
./filemanage upload --file /path/to/file.txt

# List all files
./filemanage list

# Download a file
./filemanage get --id <uuid> --output ./downloaded_file

# Delete a file
./filemanage delete --id <uuid>
```

By default the CLI talks to `http://localhost:8080` (Storage) and `http://localhost:8081` (Metadata). Override with flags:
```bash
./filemanage --storage-url http://myhost:8080 --metadata-url http://myhost:8081 upload --file myfile
```

---

## Running Tests

### Unit tests (no external dependencies)

```bash
# Storage service
cd storage-service && go test ./... -race

# Metadata service
cd metadata-service && go test ./... -race
```

### Integration tests (requires running services)

Start the stack first (`docker compose up -d`), then:

```bash
cd notification-service && go test ./... -tags=integration -race
```

---

## MinIO Console

Access the MinIO web console at [http://localhost:9001](http://localhost:9001):
- Username: `minioadmin`
- Password: `minioadmin`
