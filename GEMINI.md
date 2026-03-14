# HTTP Proxy Logger

## Project Overview
HTTP Proxy Logger is a single-binary Go reverse proxy designed for debugging and inspecting HTTP traffic. It intercepts requests and responses, decompresses bodies (gzip, deflate, brotli), and logs them to stdout with ANSI-colored syntax highlighting for JSON, XML, and HTTP headers.

- **Primary Language:** Go (1.23+)
- **Core Technologies:** `net/http`, `net/http/httputil`, `encoding/json`, `encoding/xml`.
- **Architecture:** Flat, single-package (`package main`) structure.
- **Key Components:**
  - `main.go`: Entry point, CLI flag parsing, and the `DebugTransport` (custom `http.RoundTripper`) that intercepts and logs traffic.
  - `highlight.go`: Contains all ANSI color highlighting logic for JSON, XML, and HTTP headers.

## Building and Running

### Commands
- **Build Binary:** `go build -o http-proxy-logger`
- **Run Tests:** `go test -v ./...`
- **Run Locally:** `./http-proxy-logger -target http://example.com -port 1338`
- **Build Docker Image:** `docker build -t stn1slv/http-proxy-logger .`

### Configuration
Configuration is resolved in the following order: CLI flag → Environment Variable → Default value.

| Parameter | Flag | Env Var | Default |
|-----------|------|---------|---------|
| Target URL| `-target` | `TARGET` | `http://example.com` |
| Listen Port| `-port` | `PORT` | `1338` |
| Log Requests| `-requests` | N/A | `true` |
| Log Responses| `-responses` | N/A | `true` |
| Disable Color| `-no-color` | N/A | `false` |

## Development Conventions

### Coding Style
- **Standard Library:** The project heavily relies on the Go standard library, especially for networking and data serialization.
- **Concurrency:** Uses `sync/atomic` for request counting to ensure thread-safe logging.
- **Error Handling:** Errors are handled explicitly. Panics are only used for critical startup failures (e.g., `http.ListenAndServe`).
- **Formatting:** Code should follow standard `gofmt` conventions.

### Testing Practices
- **Framework:** Uses the standard `testing` library. No external assertion libraries are used.
- **Table-Driven Tests:** Extensively used for testing highlighting logic (see `json_test.go`, `xml_test.go`).
- **Isolation:** Tests are not parallelized (`t.Parallel()` is avoided) due to the shared global `noColor` flag state.
- **Manual Verification:** Some tests manually toggle the `noColor` flag to verify both plain and colored output.

### Technical Notes
- **Interception:** The proxy uses `httputil.NewSingleHostReverseProxy` with a custom `Director` and `Transport`.
- **Decompression:** Supports `gzip`, `deflate` (zlib), and `br` (Brotli). Brotli support is provided by `github.com/andybalholm/brotli`.
- **Syntax Highlighting:**
  - JSON: Unmarshaled to `interface{}` then recursively traversed for colored pretty-printing.
  - XML: Uses a two-pass `xml.Decoder` approach to preserve namespace prefixes and handle indentation.
  - Headers: Parsed as strings and wrapped with ANSI escape codes based on the line type (Request vs. Response) and field.
