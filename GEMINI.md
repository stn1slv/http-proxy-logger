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

The `NO_COLOR` environment variable follows the [no-color.org](https://no-color.org/) convention — any non-empty value disables colored output; an empty value is ignored. Because the CLI flag has higher precedence, an explicit `-no-color=false` keeps colors on even when `NO_COLOR` is set (`resolveNoColor` detects explicit flags via `flag.FlagSet.Visit`).

The listen port must parse as a number in `0..65535`; `validateListenPort` rejects anything else (including an empty `PORT`) at startup instead of silently binding a random port.

## Development Conventions

### Coding Style
- **Standard Library:** The project heavily relies on the Go standard library, especially for networking and data serialization.
- **Concurrency:** Uses `atomic.Int64` for request counting to ensure thread-safe logging.
- **Error Handling:** Errors are handled explicitly. `log.Fatal`/`log.Fatalf` is used for critical startup failures.
- **Formatting:** Code should follow `gofumpt` conventions (stricter superset of `gofmt`).
- **Linting:** golangci-lint v2 with `.golangci.yml` config (16 linters enabled).
- **Log Body Limit:** `maxLogBodySize` (1 MB) bounds log output only; the full body is always proxied to the client. Decompression is capped at the same limit (`readLimited`), so a compressed payload that expands to gigabytes cannot exhaust memory. `formatBodyForLog` is the single place where a captured body is decoded, truncated and highlighted, and it never returns an error: a logging problem must not break the proxied request.
- **Configuration State:** The flag-backed settings (`logRequests`, `logResponses`, `cliTarget`, `cliPort`, `noColor`) are plain package-level values bound by `registerFlags` from `main`, not `flag.Bool` pointers. They are written once at startup and only read afterwards.

### Testing Practices
- **Framework:** Uses the standard `testing` library. No external assertion libraries are used.
- **Table-Driven Tests:** Extensively used for body decoding, highlighting, and config helpers.
- **Test Files:** `main_test.go` (transport, decoding, config, helpers), `highlight_test.go` (colors, headers), `json_test.go`, `xml_test.go`.
- **Isolation:** Tests are not parallelized (`t.Parallel()` is avoided) because the configuration variables are package-level shared state. Any test that changes one **must** use the `setBool` / `setString` / `setNoColor` helpers in `main_test.go`, which restore the previous value via `t.Cleanup`. Restoring by hand is what previously leaked state between tests and made the suite order-dependent.
- **Shuffling:** `make test` runs `go test -race -shuffle=on`, and so does CI. This is the regression guard for the test-isolation rule above; do not remove it.
- **Fakes:** `fakeTransport` (injected through `DebugTransport.Transport`) and `recordingConn` allow testing responses that cannot be produced by `httptest`, such as `101 Switching Protocols`. `captureLog` redirects the standard logger so notices can be asserted.
- **HTTP Testing:** Uses `net/http/httptest` for testing the `DebugTransport` round-trip behavior.

### Technical Notes
- **Proxy:** Uses `httputil.ReverseProxy` with the `Rewrite` callback and a custom `Transport` (`DebugTransport`).
- **Server:** Uses `http.Server` with `ReadHeaderTimeout` and `IdleTimeout`. There is deliberately **no** `ReadTimeout` or `WriteTimeout`: this proxy fronts arbitrary traffic and a write deadline would truncate long downloads. `serve` handles `SIGINT`/`SIGTERM` and calls `srv.Shutdown`, which waits for in-flight requests but not for hijacked connections.
- **Forwarding:** The `Rewrite` hook calls `pr.SetURL(target)` (which also points the outbound `Host` header at the target) and `pr.SetXForwarded()`.
- **Streaming Exceptions:** `RoundTrip` buffers the response body to log it, except for three cases returned before the deferred `Close`: `101 Switching Protocols` (the body is the hijacked connection and `httputil.ReverseProxy` asserts it back to `io.ReadWriteCloser`), `text/event-stream`, and `-responses=false`. The order of these early returns is load-bearing.
- **Request Capture:** `dumpRequestHeaders` dumps a *copy* of the request carrying no body. This is a safety requirement, not an optimization: `httputil.DumpRequestOut` writes a dummy body of `ContentLength` bytes into its buffer before slicing it off, so dumping the request itself lets a client allocate gigabytes merely by declaring a `Content-Length` it never sends. `restoreContentLength` puts the real length back. `captureRequestBody` then reads only the logged prefix and splices it back in front of the remainder with `readCloser`, so the upstream still receives every byte. The capture runs before the request is forwarded, so `-requests=false` is the way to let bodies stream through untouched.
- **Indentation Bound:** `indentFor` caps nesting at `maxIndentDepth` (32) for both highlighters. An indent string is written per token, so an uncapped depth makes output quadratic: 112 KB of `<a><a>...` expands to ~500 MB.
- **Decompression:** Supports `gzip`/`x-gzip`, `deflate`/`x-deflate` (zlib), and `br` (Brotli), provided by `github.com/andybalholm/brotli`. `Content-Encoding` may list several codings; they are undone in reverse order per RFC 9110 §8.4. Unsupported codings and decode failures produce an inline notice in the log rather than a wall of binary.
- **Docker:** Multi-stage build with `gcr.io/distroless/static` final image, runs as non-root user.
- **CI:** GitHub Actions — lint (golangci-lint v2), build, `gofumpt` formatting check, test with `-race -shuffle=on`. Runs on pushes to every branch, including `main`.
- **Syntax Highlighting:**
  - JSON: Unmarshaled to `interface{}` then recursively traversed for colored pretty-printing.
  - XML: Uses a two-pass `xml.Decoder` approach to preserve namespace prefixes and handle indentation.
  - Headers: Parsed as strings and wrapped with ANSI escape codes based on the line type (Request vs. Response) and field.
