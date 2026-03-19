package main

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/andybalholm/brotli"
)

// maxLogBodySize is the maximum response body size (in bytes) that will be highlighted in log output.
// Bodies exceeding this limit are replaced with a truncation notice to avoid expensive formatting.
// Note: the full response body is still buffered in memory for proxying regardless of this limit.
const maxLogBodySize = 1 << 20 // 1 MB

// reqCounter is a global atomic counter for request/response pairs.
var reqCounter atomic.Int64

// Command-line flags for controlling logging and proxy configuration.
var logRequests = flag.Bool("requests", true, "log HTTP requests")
var logResponses = flag.Bool("responses", true, "log HTTP responses")
var cliTarget = flag.String("target", "", "upstream target URL (overrides TARGET)")
var cliPort = flag.String("port", "", "listen port (overrides PORT)")
var noColor = flag.Bool("no-color", false, "disable colored output")

// DebugTransport is a custom http.RoundTripper that logs requests and responses.
type DebugTransport struct{}

// decodeBody decompresses the body if the encoding is gzip, deflate, or br.
// Returns the decoded body or the original if no decoding is needed.
func decodeBody(encoding string, body []byte) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "gzip":
		r, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		defer func() { _ = r.Close() }()
		return io.ReadAll(r)
	case "deflate":
		r, err := zlib.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		defer func() { _ = r.Close() }()
		return io.ReadAll(r)
	case "br":
		r := brotli.NewReader(bytes.NewReader(body))
		return io.ReadAll(r)
	default:
		return body, nil
	}
}

// RoundTrip implements the http.RoundTripper interface.
// It logs the outgoing request and incoming response with highlighted output.
func (DebugTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	counter := reqCounter.Add(1)

	requestDump, err := httputil.DumpRequestOut(r, true)
	if err != nil {
		return nil, err
	}
	headers := requestDump
	var body []byte
	if idx := bytes.Index(requestDump, []byte("\r\n\r\n")); idx != -1 {
		headers = requestDump[:idx]
		body = requestDump[idx+4:]
		body = highlightBody(body, r.Header.Get("Content-Type"))
	}
	headers = append(highlightHeaders(headers, true), []byte("\r\n\r\n")...)
	if *logRequests {
		line := wrapColor(fmt.Sprintf("--- REQUEST %d ---", counter), colorReqMarker)
		log.Printf("%s %s\n\n%s%s\n\n", coloredTime(time.Now(), colorReqMarker), line, string(headers), string(body))
	}

	response, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	bodyBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	// restore body for client
	response.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	headerDump, err := httputil.DumpResponse(response, false)
	if err != nil {
		return nil, err
	}

	var decoded []byte
	if len(bodyBytes) > maxLogBodySize {
		decoded = []byte(fmt.Sprintf("[body too large to display: %d bytes]", len(bodyBytes)))
	} else {
		decoded, err = decodeBody(response.Header.Get("Content-Encoding"), bodyBytes)
		if err != nil {
			decoded = bodyBytes
		}
		decoded = highlightBody(decoded, response.Header.Get("Content-Type"))
	}

	headerDump = append(highlightHeaders(bytes.TrimSuffix(headerDump, []byte("\r\n\r\n")), false), []byte("\r\n\r\n")...)

	if *logResponses {
		line := wrapColor(fmt.Sprintf("--- RESPONSE %d (%s) ---", counter, response.Status), colorResMarker)
		log.Printf("%s %s\n\n%s%s\n\n", coloredTime(time.Now(), colorResMarker), line, string(headerDump), string(decoded))
	}
	// restore body again for proxying
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

// getListenAddress returns the address to listen on, using CLI flag, env, or default.
func getListenAddress() string {
	port := *cliPort
	if port == "" {
		port = getEnv("PORT", "1338")
	}
	return ":" + port
}

// getTarget returns the upstream target URL, using CLI flag, env, or default.
func getTarget() string {
	target := *cliTarget
	if target == "" {
		target = getEnv("TARGET", "http://example.com")
	}
	return target
}

// main is the entry point. It sets up the reverse proxy and starts the HTTP server.
func main() {
	flag.Parse()
	// Respect NO_COLOR env var convention (https://no-color.org/)
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		*noColor = true
	}
	log.SetFlags(0)
	rawTarget := getTarget()
	target, err := url.Parse(rawTarget)
	if err != nil {
		log.Fatalf("invalid target URL %q: %v", rawTarget, err)
	}
	if target.Scheme == "" || target.Host == "" {
		log.Fatalf("invalid target URL %q: scheme and host are required", rawTarget)
	}
	log.Printf("%s %s -> %s\n", coloredTime(time.Now(), colorTime), getListenAddress(), target)

	proxy := &httputil.ReverseProxy{
		Transport: DebugTransport{},
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.Host = target.Host
		},
	}

	srv := &http.Server{
		Addr:         getListenAddress(),
		Handler:      proxy,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
