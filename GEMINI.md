# HTTP Proxy Logger

## Project Overview
HTTP Proxy Logger is a single-binary Go reverse proxy designed for debugging and inspecting HTTP traffic. It intercepts requests and responses, decompresses bodies (gzip, deflate, brotli), and logs them to stdout with ANSI-colored syntax highlighting for JSON, XML, and HTTP headers.

- **Primary Language:** Go (1.26+)
- **Core Technologies:** `net/http`, `net/http/httputil`, `encoding/json`, `encoding/xml`.
- **Architecture:** Flat, single-package (`package main`) structure.
- **Key Components:**
  - `main.go`: Entry point, CLI flag parsing, `http.Server` with timeouts, and the `DebugTransport` (custom `http.RoundTripper`) that intercepts and logs traffic.
  - `highlight.go`: Contains all ANSI color highlighting logic for JSON, XML, and HTTP headers.
  - `main_test.go`: Tests for proxy transport, body decoding, and configuration helpers.
  - `highlight_test.go`: Tests for header/status highlighting and color utilities.

## Building and Running

### Commands (via Makefile)
- **Setup:** `make setup` — downloads dependencies, prints tool install instructions.
- **Build:** `make build` — produces a static binary (`CGO_ENABLED=0`).
- **Test:** `make test` — runs `go test -race -v ./...`.
- **Lint:** `make lint` — runs `golangci-lint run ./...`.
- **Format:** `make format` — runs `gofumpt -extra -w .`.
- **Run:** `make run` — builds and runs the binary.
- **Docker:** `docker build -t stn1slv/http-proxy-logger .`

### Configuration
Configuration is resolved in the following order: CLI flag → Environment Variable → Default value.

| Parameter | Flag | Env Var | Default |
|-----------|------|---------|---------|
| Target URL| `-target` | `TARGET` | `http://example.com` |
| Listen Port| `-port` | `PORT` | `1338` |
| Log Requests| `-requests` | N/A | `true` |
| Log Responses| `-responses` | N/A | `true` |
| Disable Color| `-no-color` | `NO_COLOR` | `false` |

The `NO_COLOR` environment variable follows the [no-color.org](https://no-color.org/) convention — when set (any value), colored output is disabled.

## Development Conventions

### Coding Style
- **Standard Library:** The project heavily relies on the Go standard library, especially for networking and data serialization.
- **Concurrency:** Uses `atomic.Int64` for request counting to ensure thread-safe logging.
- **Error Handling:** Errors are handled explicitly. `log.Fatal`/`log.Fatalf` is used for critical startup failures.
- **Formatting:** Code should follow `gofumpt` conventions (stricter superset of `gofmt`).
- **Linting:** golangci-lint v2 with `.golangci.yml` config (16 linters enabled).
- **Log Body Limit:** Response bodies exceeding `maxLogBodySize` (1 MB) are replaced with a truncation notice in log output; the full body is still proxied to the client.

### Testing Practices
- **Framework:** Uses the standard `testing` library. No external assertion libraries are used.
- **Table-Driven Tests:** Extensively used for body decoding, highlighting, and config helpers.
- **Test Files:** `main_test.go` (transport, decoding, config), `highlight_test.go` (colors, headers), `json_test.go`, `xml_test.go`.
- **Isolation:** Tests are not parallelized (`t.Parallel()` is avoided) due to the shared global `noColor` flag state.
- **Manual Verification:** Some tests manually toggle the `noColor` flag to verify both plain and colored output.
- **HTTP Testing:** Uses `net/http/httptest` for testing the `DebugTransport` round-trip behavior.

### Technical Notes
- **Proxy:** Uses `httputil.ReverseProxy` with the `Rewrite` callback and a custom `Transport` (`DebugTransport`).
- **Server:** Uses `http.Server` with explicit `ReadTimeout`, `WriteTimeout`, and `IdleTimeout`.
- **Decompression:** Supports `gzip`, `deflate` (zlib), and `br` (Brotli). Brotli support is provided by `github.com/andybalholm/brotli`.
- **Docker:** Multi-stage build with `gcr.io/distroless/static` final image, runs as non-root user.
- **CI:** GitHub Actions — lint (golangci-lint v2), build, test with `-race`.
- **Syntax Highlighting:**
  - JSON: Unmarshaled to `interface{}` then recursively traversed for colored pretty-printing.
  - XML: Uses a two-pass `xml.Decoder` approach to preserve namespace prefixes and handle indentation.
  - Headers: Parsed as strings and wrapped with ANSI escape codes based on the line type (Request vs. Response) and field.
