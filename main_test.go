package main

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
)

// compressGzip returns gzip-compressed bytes.
func compressGzip(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// compressDeflate returns zlib-compressed bytes.
func compressDeflate(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// compressBrotli returns brotli-compressed bytes.
func compressBrotli(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := brotli.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDecodeBody(t *testing.T) {
	original := []byte("hello, world!")

	tests := []struct {
		name     string
		encoding string
		body     []byte
		want     string
		wantErr  bool
	}{
		{
			name:     "gzip",
			encoding: "gzip",
			body:     compressGzip(t, original),
			want:     "hello, world!",
		},
		{
			name:     "gzip with whitespace",
			encoding: "  Gzip  ",
			body:     compressGzip(t, original),
			want:     "hello, world!",
		},
		{
			name:     "deflate",
			encoding: "deflate",
			body:     compressDeflate(t, original),
			want:     "hello, world!",
		},
		{
			name:     "brotli",
			encoding: "br",
			body:     compressBrotli(t, original),
			want:     "hello, world!",
		},
		{
			name:     "no encoding",
			encoding: "",
			body:     original,
			want:     "hello, world!",
		},
		{
			name:     "unknown encoding",
			encoding: "identity",
			body:     original,
			want:     "hello, world!",
		},
		{
			name:     "invalid gzip data",
			encoding: "gzip",
			body:     []byte("not gzip"),
			wantErr:  true,
		},
		{
			name:     "invalid deflate data",
			encoding: "deflate",
			body:     []byte("not deflate"),
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeBody(tt.encoding, tt.body)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("got %q, want %q", string(got), tt.want)
			}
		})
	}
}

func TestRoundTrip(t *testing.T) {
	// Ensure noColor is set for predictable output
	originalNoColor := noColor
	if noColor == nil {
		noColor = flag.Bool("no-color-rt", false, "")
	}
	defer func() { noColor = originalNoColor }()
	*noColor = true

	t.Run("proxies response correctly", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"status":"ok"}`)
		}))
		defer upstream.Close()

		transport := DebugTransport{}
		req, err := http.NewRequest(http.MethodGet, upstream.URL+"/test", nil)
		if err != nil {
			t.Fatal(err)
		}

		resp, err := transport.RoundTrip(req)
		if err != nil {
			t.Fatalf("RoundTrip failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("got status %d, want %d", resp.StatusCode, http.StatusOK)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != `{"status":"ok"}` {
			t.Errorf("body not preserved for proxying: got %q", string(body))
		}
	})

	t.Run("proxies gzip response", func(t *testing.T) {
		original := `{"compressed":true}`
		compressed := compressGzip(t, []byte(original))

		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Encoding", "gzip")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(compressed)
		}))
		defer upstream.Close()

		transport := DebugTransport{}
		req, err := http.NewRequest(http.MethodGet, upstream.URL, nil)
		if err != nil {
			t.Fatal(err)
		}

		resp, err := transport.RoundTrip(req)
		if err != nil {
			t.Fatalf("RoundTrip failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		// Go's DefaultTransport auto-decompresses gzip and strips Content-Encoding,
		// so the body available to the client is already decompressed.
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != original {
			t.Errorf("got %q, want %q", string(body), original)
		}
	})

	t.Run("handles POST with request body", func(t *testing.T) {
		var receivedBody string
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			receivedBody = string(b)
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprint(w, "created")
		}))
		defer upstream.Close()

		transport := DebugTransport{}
		req, err := http.NewRequest(http.MethodPost, upstream.URL, strings.NewReader(`{"name":"test"}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := transport.RoundTrip(req)
		if err != nil {
			t.Fatalf("RoundTrip failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			t.Errorf("got status %d, want %d", resp.StatusCode, http.StatusCreated)
		}
		if receivedBody != `{"name":"test"}` {
			t.Errorf("upstream received %q, want %q", receivedBody, `{"name":"test"}`)
		}
	})

	t.Run("handles upstream error", func(t *testing.T) {
		transport := DebugTransport{}
		req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:1", nil)
		if err != nil {
			t.Fatal(err)
		}

		_, err = transport.RoundTrip(req)
		if err == nil {
			t.Error("expected error for unreachable upstream, got nil")
		}
	})

	t.Run("large body triggers truncation message", func(t *testing.T) {
		// Create a response body larger than maxLogBodySize
		largeBody := strings.Repeat("x", maxLogBodySize+1)

		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, largeBody)
		}))
		defer upstream.Close()

		transport := DebugTransport{}
		req, err := http.NewRequest(http.MethodGet, upstream.URL, nil)
		if err != nil {
			t.Fatal(err)
		}

		resp, err := transport.RoundTrip(req)
		if err != nil {
			t.Fatalf("RoundTrip failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		// Body should still be fully preserved for the client
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if len(body) != maxLogBodySize+1 {
			t.Errorf("body length = %d, want %d", len(body), maxLogBodySize+1)
		}
	})

	t.Run("logging flags are respected", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer upstream.Close()

		// Disable both logging flags
		origReq := *logRequests
		origResp := *logResponses
		*logRequests = false
		*logResponses = false
		defer func() {
			*logRequests = origReq
			*logResponses = origResp
		}()

		transport := DebugTransport{}
		req, err := http.NewRequest(http.MethodGet, upstream.URL, nil)
		if err != nil {
			t.Fatal(err)
		}

		resp, err := transport.RoundTrip(req)
		if err != nil {
			t.Fatalf("RoundTrip failed: %v", err)
		}
		_ = resp.Body.Close()

		// No assertion on log output — just verify it doesn't panic with logging disabled
	})
}

func TestGetEnv(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		envVal   string
		setEnv   bool
		fallback string
		want     string
	}{
		{
			name:     "returns env value when set",
			key:      "TEST_HTTP_PROXY_LOGGER_VAR",
			envVal:   "from-env",
			setEnv:   true,
			fallback: "default",
			want:     "from-env",
		},
		{
			name:     "returns fallback when not set",
			key:      "TEST_HTTP_PROXY_LOGGER_UNSET",
			setEnv:   false,
			fallback: "default",
			want:     "default",
		},
		{
			name:     "returns empty string when env is empty",
			key:      "TEST_HTTP_PROXY_LOGGER_EMPTY",
			envVal:   "",
			setEnv:   true,
			fallback: "default",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv(tt.key, tt.envVal)
			} else {
				_ = os.Unsetenv(tt.key)
			}
			got := getEnv(tt.key, tt.fallback)
			if got != tt.want {
				t.Errorf("getEnv(%q, %q) = %q, want %q", tt.key, tt.fallback, got, tt.want)
			}
		})
	}
}

func TestGetListenAddress(t *testing.T) {
	// Save and restore original flag value
	origPort := *cliPort
	defer func() { *cliPort = origPort }()

	t.Run("uses CLI flag when set", func(t *testing.T) {
		*cliPort = "9090"
		got := getListenAddress()
		if got != ":9090" {
			t.Errorf("got %q, want %q", got, ":9090")
		}
	})

	t.Run("uses env when flag is empty", func(t *testing.T) {
		*cliPort = ""
		t.Setenv("PORT", "8080")
		got := getListenAddress()
		if got != ":8080" {
			t.Errorf("got %q, want %q", got, ":8080")
		}
	})

	t.Run("uses default when both empty", func(t *testing.T) {
		*cliPort = ""
		_ = os.Unsetenv("PORT")
		got := getListenAddress()
		if got != ":1338" {
			t.Errorf("got %q, want %q", got, ":1338")
		}
	})
}

func TestGetTarget(t *testing.T) {
	// Save and restore original flag value
	origTarget := *cliTarget
	defer func() { *cliTarget = origTarget }()

	t.Run("uses CLI flag when set", func(t *testing.T) {
		*cliTarget = "http://myserver.com"
		got := getTarget()
		if got != "http://myserver.com" {
			t.Errorf("got %q, want %q", got, "http://myserver.com")
		}
	})

	t.Run("uses env when flag is empty", func(t *testing.T) {
		*cliTarget = ""
		t.Setenv("TARGET", "http://envserver.com")
		got := getTarget()
		if got != "http://envserver.com" {
			t.Errorf("got %q, want %q", got, "http://envserver.com")
		}
	})

	t.Run("uses default when both empty", func(t *testing.T) {
		*cliTarget = ""
		_ = os.Unsetenv("TARGET")
		got := getTarget()
		if got != "http://example.com" {
			t.Errorf("got %q, want %q", got, "http://example.com")
		}
	})
}

func TestRoundTripPreservesJSONBody(t *testing.T) {
	// Verify that JSON response body is preserved exactly for proxying,
	// even though it gets reformatted for logging.
	originalNoColor := noColor
	if noColor == nil {
		noColor = flag.Bool("no-color-json", false, "")
	}
	defer func() { noColor = originalNoColor }()
	*noColor = true

	payload := map[string]interface{}{
		"users": []interface{}{
			map[string]interface{}{"id": float64(1), "name": "Alice"},
			map[string]interface{}{"id": float64(2), "name": "Bob"},
		},
	}
	responseJSON, _ := json.Marshal(payload)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(responseJSON)
	}))
	defer upstream.Close()

	transport := DebugTransport{}
	req, err := http.NewRequest(http.MethodGet, upstream.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(body, responseJSON) {
		t.Errorf("JSON body not preserved.\ngot:  %s\nwant: %s", body, responseJSON)
	}
}
