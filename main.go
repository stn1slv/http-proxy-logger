package main

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/andybalholm/brotli"
)

// maxLogBodySize is the maximum body size (in bytes) shown in log output.
// Larger bodies are replaced with a truncation notice, and decompression stops
// once this much output has been produced so a decompression bomb cannot
// exhaust memory. The full body is still proxied regardless of this limit.
const maxLogBodySize = 1 << 20 // 1 MB

// Content codings understood by decodeBody.
const (
	encodingGzip     = "gzip"
	encodingXGzip    = "x-gzip"
	encodingDeflate  = "deflate"
	encodingXDeflate = "x-deflate"
	encodingBrotli   = "br"
	encodingIdentity = "identity"
)

// Fallbacks used when neither a CLI flag nor an environment variable is set.
const (
	defaultPort   = "1338"
	defaultTarget = "http://example.com"
)

// reqCounter is a global atomic counter for request/response pairs.
var reqCounter atomic.Int64

// Configuration values bound to command-line flags by registerFlags.
// They are written once during startup and only read afterwards.
var (
	logRequests  = true
	logResponses = true
	cliTarget    string
	cliPort      string
	noColor      bool
)

// registerFlags binds the configuration variables to fs. It is called from main
// so that test binaries keep a clean flag set.
func registerFlags(fs *flag.FlagSet) {
	fs.BoolVar(&logRequests, "requests", true, "log HTTP requests")
	fs.BoolVar(&logResponses, "responses", true, "log HTTP responses")
	fs.StringVar(&cliTarget, "target", "", "upstream target URL (overrides TARGET)")
	fs.StringVar(&cliPort, "port", "", "listen port (overrides PORT)")
	fs.BoolVar(&noColor, "no-color", false, "disable colored output")
}

// readCloser pairs a Reader with a Closer belonging to a different value, so a
// partially consumed body can be spliced back together without losing its Close.
type readCloser struct {
	io.Reader
	io.Closer
}

// DebugTransport is a custom http.RoundTripper that logs requests and responses.
type DebugTransport struct {
	// Transport reaches the upstream server. When nil, http.DefaultTransport is used.
	Transport http.RoundTripper
}

// upstream returns the RoundTripper used to reach the upstream server.
func (t DebugTransport) upstream() http.RoundTripper {
	if t.Transport != nil {
		return t.Transport
	}
	return http.DefaultTransport
}

// splitEncodings splits a Content-Encoding header value into its normalized
// tokens, dropping the no-op "identity" coding.
func splitEncodings(encoding string) []string {
	var tokens []string
	for _, tok := range strings.Split(encoding, ",") {
		tok = strings.ToLower(strings.TrimSpace(tok))
		if tok == "" || tok == encodingIdentity {
			continue
		}
		tokens = append(tokens, tok)
	}
	return tokens
}

// supportedEncoding reports whether decodeBody can decompress the given coding.
func supportedEncoding(token string) bool {
	switch token {
	case encodingGzip, encodingXGzip, encodingDeflate, encodingXDeflate, encodingBrotli:
		return true
	}
	return false
}

// unsupportedEncodings returns the codings in a Content-Encoding value that
// cannot be decompressed, so the log can explain why a body looks like noise.
func unsupportedEncodings(encoding string) []string {
	var out []string
	for _, tok := range splitEncodings(encoding) {
		if !supportedEncoding(tok) {
			out = append(out, tok)
		}
	}
	return out
}

// readLimited reads at most maxLogBodySize+1 bytes, so callers can detect that
// the source was larger with len(out) > maxLogBodySize.
func readLimited(r io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, maxLogBodySize+1))
}

// decodeOnce applies a single content coding. Unsupported codings return the
// body unchanged. Output is capped, so a decompression bomb cannot exhaust memory.
func decodeOnce(token string, body []byte) ([]byte, error) {
	switch token {
	case encodingGzip, encodingXGzip:
		r, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		defer func() { _ = r.Close() }()
		return readLimited(r)
	case encodingDeflate, encodingXDeflate:
		r, err := zlib.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		defer func() { _ = r.Close() }()
		return readLimited(r)
	case encodingBrotli:
		return readLimited(brotli.NewReader(bytes.NewReader(body)))
	default:
		return body, nil
	}
}

// decodeBody decompresses body according to a Content-Encoding header value,
// which may list several codings. RFC 9110 section 8.4 lists codings in the
// order they were applied, so they are undone in reverse. Unsupported codings
// leave the body unchanged.
func decodeBody(encoding string, body []byte) ([]byte, error) {
	tokens := splitEncodings(encoding)
	out := body
	for i := len(tokens) - 1; i >= 0; i-- {
		if !supportedEncoding(tokens[i]) {
			break
		}
		decoded, err := decodeOnce(tokens[i], out)
		if err != nil {
			return nil, err
		}
		out = decoded
		if len(out) > maxLogBodySize {
			// Already past the log limit; decoding further only wastes memory.
			break
		}
	}
	return out, nil
}

// decodeForLog decompresses body for display. It returns the bytes to show,
// whether decompression was applied, and a notice explaining any problem.
func decodeForLog(body []byte, contentEncoding string) (out []byte, decompressed bool, notice string) {
	if len(splitEncodings(contentEncoding)) == 0 {
		return body, false, ""
	}
	if unsupported := unsupportedEncodings(contentEncoding); len(unsupported) > 0 {
		return body, false, fmt.Sprintf("\n[not decoded: unsupported content-encoding %q]", strings.Join(unsupported, ", "))
	}
	decoded, err := decodeBody(contentEncoding, body)
	if err != nil {
		return body, false, fmt.Sprintf("\n[decode failed: %v]", err)
	}
	return decoded, true, ""
}

// formatBodyForLog prepares a captured body for log output: it decompresses when
// possible, replaces oversized bodies with a notice, and applies highlighting.
// wireSize is the encoded length of the full body, or -1 when it is unknown.
// It never fails, because a logging problem must not break proxying.
func formatBodyForLog(body []byte, wireSize int64, contentEncoding, contentType string) []byte {
	decoded, decompressed, notice := decodeForLog(body, contentEncoding)

	if len(decoded) > maxLogBodySize {
		switch {
		case decompressed:
			return fmt.Appendf(nil, "[decompressed body exceeds the %d byte log limit]%s", maxLogBodySize, notice)
		case wireSize >= 0:
			return fmt.Appendf(nil, "[body too large to display: %d bytes]%s", wireSize, notice)
		default:
			return fmt.Appendf(nil, "[body too large to display: over %d bytes]%s", maxLogBodySize, notice)
		}
	}
	return append(highlightBody(decoded, contentType), notice...)
}

// isEventStream reports whether the response is a server-sent event stream,
// which must reach the client as it arrives rather than being buffered.
func isEventStream(response *http.Response) bool {
	return strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream")
}

// captureRequestBody reads the start of the request body for logging and splices
// it back in front of the remainder, so the upstream still receives every byte.
func captureRequestBody(r *http.Request) []byte {
	if r.Body == nil || r.Body == http.NoBody {
		return nil
	}
	orig := r.Body
	head, err := readLimited(orig)
	r.Body = readCloser{Reader: io.MultiReader(bytes.NewReader(head), orig), Closer: orig}
	if err != nil {
		return fmt.Appendf(nil, "[failed to read request body: %v]", err)
	}
	return formatBodyForLog(head, r.ContentLength, r.Header.Get("Content-Encoding"), r.Header.Get("Content-Type"))
}

// logRequest logs the outgoing request. Dump failures are reported inline rather
// than returned, so a logging problem never fails the proxied request.
func logRequest(r *http.Request, counter int64) {
	// Dump the headers before touching the body: DumpRequestOut with body=false
	// substitutes a dummy body and restores the original, leaving r.Body intact,
	// and its output avoids the chunked framing that body=true would include.
	var headers []byte
	if dump, err := httputil.DumpRequestOut(r, false); err != nil {
		headers = fmt.Appendf(nil, "[failed to dump request headers: %v]\r\n\r\n", err)
	} else {
		headers = append(highlightHeaders(bytes.TrimSuffix(dump, []byte("\r\n\r\n")), true), []byte("\r\n\r\n")...)
	}

	body := captureRequestBody(r)

	line := wrapColor(fmt.Sprintf("--- REQUEST %d ---", counter), colorReqMarker)
	log.Printf("%s %s\n\n%s%s\n\n", coloredTime(time.Now(), colorReqMarker), line, headers, body)
}

// logResponse logs the incoming response. Pass a nil body for responses that are
// streamed straight through to the client.
func logResponse(response *http.Response, body []byte, counter int64) {
	var headers []byte
	if dump, err := httputil.DumpResponse(response, false); err != nil {
		headers = fmt.Appendf(nil, "[failed to dump response headers: %v]\r\n\r\n", err)
	} else {
		headers = append(highlightHeaders(bytes.TrimSuffix(dump, []byte("\r\n\r\n")), false), []byte("\r\n\r\n")...)
	}

	line := wrapColor(fmt.Sprintf("--- RESPONSE %d (%s) ---", counter, response.Status), colorResMarker)
	log.Printf("%s %s\n\n%s%s\n\n", coloredTime(time.Now(), colorResMarker), line, headers, body)
}

// RoundTrip implements the http.RoundTripper interface.
// It logs the outgoing request and incoming response with highlighted output.
func (t DebugTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	counter := reqCounter.Add(1)

	if logRequests {
		logRequest(r, counter)
	}

	response, err := t.upstream().RoundTrip(r)
	if err != nil {
		return nil, err
	}

	// These responses are forwarded without buffering. The early returns must
	// stay above the deferred Close below, which would otherwise close a body
	// that is being handed back to the caller.
	switch {
	case !logResponses:
		return response, nil
	case response.StatusCode == http.StatusSwitchingProtocols:
		// The body is the hijacked connection. httputil.ReverseProxy requires it
		// to remain an io.ReadWriteCloser, and reading it would block until the
		// peer disconnects.
		logResponse(response, []byte("[connection upgraded: body not captured]"), counter)
		return response, nil
	case isEventStream(response):
		// Buffering an event stream would withhold every event until the
		// upstream closed the connection.
		logResponse(response, []byte("[event stream: body not captured]"), counter)
		return response, nil
	}

	origBody := response.Body
	defer func() { _ = origBody.Close() }()

	bodyBytes, err := io.ReadAll(origBody)
	if err != nil {
		return nil, err
	}

	logResponse(response, formatBodyForLog(bodyBytes, int64(len(bodyBytes)),
		response.Header.Get("Content-Encoding"), response.Header.Get("Content-Type")), counter)

	response.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	return response, nil
}

// getEnv returns the value of the environment variable or a fallback if not set.
func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

// getListenPort returns the port to listen on, using CLI flag, env, or default.
func getListenPort() string {
	port := cliPort
	if port == "" {
		port = getEnv("PORT", defaultPort)
	}
	return port
}

// getListenAddress returns the address to listen on, using CLI flag, env, or default.
func getListenAddress() string {
	return ":" + getListenPort()
}

// validateListenPort reports an error when the resolved port cannot be used. An
// empty or malformed port would otherwise silently bind a random free port.
func validateListenPort() error {
	port := getListenPort()
	n, err := strconv.Atoi(port)
	if err != nil || n < 0 || n > 65535 {
		return fmt.Errorf("invalid listen port %q: expected a number between 0 and 65535", port)
	}
	return nil
}

// getTarget returns the upstream target URL, using CLI flag, env, or default.
func getTarget() string {
	target := cliTarget
	if target == "" {
		target = getEnv("TARGET", defaultTarget)
	}
	return target
}

// resolveNoColor applies the NO_COLOR convention (https://no-color.org/) unless
// -no-color was passed explicitly. This project documents CLI flag > env var >
// default, so an explicit -no-color=false keeps colors on even when NO_COLOR is
// set. Per the convention, an empty NO_COLOR value is ignored.
func resolveNoColor(fs *flag.FlagSet, current bool) bool {
	explicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "no-color" {
			explicit = true
		}
	})
	if explicit {
		return current
	}
	if os.Getenv("NO_COLOR") != "" {
		return true
	}
	return current
}

// main is the entry point. It sets up the reverse proxy and starts the HTTP server.
func main() {
	registerFlags(flag.CommandLine)
	flag.Parse()
	noColor = resolveNoColor(flag.CommandLine, noColor)
	log.SetFlags(0)

	rawTarget := getTarget()
	target, err := url.Parse(rawTarget)
	if err != nil {
		log.Fatalf("invalid target URL %q: %v", rawTarget, err)
	}
	if target.Scheme == "" || target.Host == "" {
		log.Fatalf("invalid target URL %q: scheme and host are required", rawTarget)
	}
	if err := validateListenPort(); err != nil {
		log.Fatal(err)
	}
	addr := getListenAddress()
	log.Printf("%s %s -> %s\n", coloredTime(time.Now(), colorTime), addr, target)

	proxy := &httputil.ReverseProxy{
		Transport: DebugTransport{},
		Rewrite: func(pr *httputil.ProxyRequest) {
			// SetURL also points the outbound Host header at the target.
			pr.SetURL(target)
			pr.SetXForwarded()
		},
	}

	srv := &http.Server{
		Addr:    addr,
		Handler: proxy,
		// No Read/Write deadline: this proxy fronts arbitrary traffic, and a
		// write deadline would truncate long downloads. ReadHeaderTimeout still
		// guards against slow-header attacks.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	if err := serve(srv); err != nil {
		log.Fatal(err)
	}
}

// serve runs srv until it fails or the process is interrupted, in which case
// in-flight requests are given time to finish.
func serve(srv *http.Server) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		stop() // a second signal now terminates the process immediately
		log.Printf("%s shutting down\n", coloredTime(time.Now(), colorTime))
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		// Shutdown waits for in-flight requests but not for hijacked
		// connections, so proxied upgrades are cut at this point.
		return srv.Shutdown(shutdownCtx)
	}
}
