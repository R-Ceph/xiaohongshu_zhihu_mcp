## Cursor Cloud specific instructions

### Project overview

xiaohongshu-mcp is a Go-based MCP server providing browser automation tools for Xiaohongshu (小红书) and Zhihu (知乎). Single binary, no external databases. See `README.md` for full feature list and client integration guides.

### Prerequisites

- **Go 1.24+** (already in VM)
- **Google Chrome** at `/usr/local/bin/google-chrome` (already in VM)

### Key commands

| Task | Command |
|------|---------|
| Install deps | `go mod download` |
| Unit tests | `go test ./pkg/... && go test -run TestFilterValidation ./xiaohongshu/` |
| Lint | `go vet ./...` |
| Build | `go build -o xiaohongshu-mcp .` |
| Run (headless) | `ROD_BROWSER_BIN=/usr/local/bin/google-chrome go run .` |
| Run (with GUI) | `ROD_BROWSER_BIN=/usr/local/bin/google-chrome go run . -headless=false` |

### Gotchas

- The `ROD_BROWSER_BIN` env var must point to Chrome; without it, `go-rod` tries to auto-download Chromium (~150 MB) on first run.
- Tests in `xiaohongshu/search_test.go` (`TestSearchWithFilters`) launch a real browser and hit the network — they will hang/fail in CI without cookies and network access. Only `TestFilterValidation` is a pure unit test. Tests in `xiaohongshu/feeds_test.go` and `xiaohongshu/publish_test.go` are `t.Skip`-ed.
- The server listens on port **18060** by default. Health: `GET /health`, MCP: `POST /mcp`.
- Login requires scanning a QR code with the Xiaohongshu mobile app (one-time, produces `cookies.json`). Without `cookies.json` most features return "not logged in" but the server still starts fine.
- Clean up the built binary after testing (it's in `.gitignore` already, but good practice).
