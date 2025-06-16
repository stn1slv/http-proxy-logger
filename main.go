package main

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"flag"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// reqCounter is a global atomic counter for request/response pairs.
var reqCounter int32

// Command-line flags for controlling logging and proxy configuration.
var logRequests = flag.Bool("requests", true, "log HTTP requests")
var logResponses = flag.Bool("responses", true, "log HTTP responses")
var cliTarget = flag.String("target", "", "upstream target URL (overrides TARGET)")
var cliPort = flag.String("port", "", "listen port (overrides PORT)")

// DebugTransport is a custom http.RoundTripper that logs requests and responses.
type DebugTransport struct{}

// decodeBody decompresses the body if the encoding is gzip or deflate.
// Returns the decoded body or the original if no decoding is needed.
func decodeBody(encoding string, body []byte) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "gzip":
		r, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		defer r.Close()
		return io.ReadAll(r)
	case "deflate":
		r, err := zlib.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		defer r.Close()
		return io.ReadAll(r)
	default:
		return body, nil
	}
}

// coloredTimeWithColor returns the formatted time string wrapped in the given color.
func coloredTimeWithColor(t time.Time, color string) string {
	return wrapColor("["+t.Format("2006/01/02 15:04:05")+"]", color)
}

// RoundTrip implements the http.RoundTripper interface.
// It logs the outgoing request and incoming response with highlighted output.
func (DebugTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	counter := atomic.AddInt32(&reqCounter, 1)

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
		line := colorReqMarker + "--- REQUEST " + strconv.Itoa(int(counter)) + " ---" + colorReset
		log.Printf("%s %s\n\n%s%s\n\n", coloredTimeWithColor(time.Now(), colorReqMarker), line, string(headers), string(body))
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
	response.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	headerDump, err := httputil.DumpResponse(response, false)
	if err != nil {
		return nil, err
	}

	decoded, err := decodeBody(response.Header.Get("Content-Encoding"), bodyBytes)
	if err != nil {
		decoded = bodyBytes
	}
	decoded = highlightBody(decoded, response.Header.Get("Content-Type"))

	headerDump = append(highlightHeaders(bytes.TrimSuffix(headerDump, []byte("\r\n\r\n")), false), []byte("\r\n\r\n")...)

	if *logResponses {
		line := colorResMarker + "--- RESPONSE " + strconv.Itoa(int(counter)) + " [" + response.Status + "] ---" + colorReset
		log.Printf("%s %s\n\n%s%s\n\n", coloredTimeWithColor(time.Now(), colorResMarker), line, string(headerDump), string(decoded))
	}
	// restore body again for proxying
	response.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
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
	log.SetFlags(0)
	target, _ := url.Parse(getTarget())
	log.Printf("%s %s -> %s\n", coloredTime(time.Now()), getListenAddress(), target)

	proxy := httputil.NewSingleHostReverseProxy(target)

	proxy.Transport = DebugTransport{}

	d := proxy.Director
	proxy.Director = func(r *http.Request) {
		d(r) // call default director

		r.Host = target.Host // set Host header as expected by target
	}

	if err := http.ListenAndServe(getListenAddress(), proxy); err != nil {
		panic(err)
	}
}
