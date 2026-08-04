package main

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
)

// The configuration variables are package-level and shared, so every test that
// changes one must restore it. These helpers make that automatic: restoring by
// hand is what previously leaked state between tests and made the suite
// order-dependent.

// setBool sets a package-level bool for the duration of the test.
func setBool(t *testing.T, target *bool, v bool) {
	t.Helper()
	orig := *target
	t.Cleanup(func() { *target = orig })
	*target = v
}

// setString sets a package-level string for the duration of the test.
func setString(t *testing.T, target *string, v string) {
	t.Helper()
	orig := *target
	t.Cleanup(func() { *target = orig })
	*target = v
}

// setNoColor controls color output for the duration of the test.
func setNoColor(t *testing.T, v bool) {
	t.Helper()
	setBool(t, &noColor, v)
}

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
	setNoColor(t, true)

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
		setBool(t, &logRequests, false)
		setBool(t, &logResponses, false)

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
	setString(t, &cliPort, "")

	t.Run("uses CLI flag when set", func(t *testing.T) {
		cliPort = "9090"
		got := getListenAddress()
		if got != ":9090" {
			t.Errorf("got %q, want %q", got, ":9090")
		}
	})

	t.Run("uses env when flag is empty", func(t *testing.T) {
		cliPort = ""
		t.Setenv("PORT", "8080")
		got := getListenAddress()
		if got != ":8080" {
			t.Errorf("got %q, want %q", got, ":8080")
		}
	})

	t.Run("uses default when both empty", func(t *testing.T) {
		cliPort = ""
		_ = os.Unsetenv("PORT")
		got := getListenAddress()
		if got != ":1338" {
			t.Errorf("got %q, want %q", got, ":1338")
		}
	})
}

func TestGetTarget(t *testing.T) {
	setString(t, &cliTarget, "")

	t.Run("uses CLI flag when set", func(t *testing.T) {
		cliTarget = "http://myserver.com"
		got := getTarget()
		if got != "http://myserver.com" {
			t.Errorf("got %q, want %q", got, "http://myserver.com")
		}
	})

	t.Run("uses env when flag is empty", func(t *testing.T) {
		cliTarget = ""
		t.Setenv("TARGET", "http://envserver.com")
		got := getTarget()
		if got != "http://envserver.com" {
			t.Errorf("got %q, want %q", got, "http://envserver.com")
		}
	})

	t.Run("uses default when both empty", func(t *testing.T) {
		cliTarget = ""
		_ = os.Unsetenv("TARGET")
		got := getTarget()
		if got != "http://example.com" {
			t.Errorf("got %q, want %q", got, "http://example.com")
		}
	})
}

func TestRoundTripPreservesJSONBody(t *testing.T) {
	// Verify that JSON response body is preserved exactly for proxying,
	setNoColor(t, true)

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

// captureLog redirects the standard logger into a buffer for the duration of
// the test, so log notices can be asserted on and test output stays quiet.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	origOut, origFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(origOut)
		log.SetFlags(origFlags)
	})
	return &buf
}

// fakeTransport returns a canned response instead of contacting a server.
type fakeTransport struct {
	response *http.Response
}

func (f fakeTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return f.response, nil
}

// recordingConn is a response body that reports whether it was read. It
// implements io.ReadWriteCloser so it can stand in for a hijacked connection.
type recordingConn struct {
	readCalled bool
}

func (c *recordingConn) Read([]byte) (int, error)    { c.readCalled = true; return 0, io.EOF }
func (c *recordingConn) Write(p []byte) (int, error) { return len(p), nil }
func (c *recordingConn) Close() error                { return nil }

// roundTripCanned runs a canned response through DebugTransport.
func roundTripCanned(t *testing.T, response *http.Response) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://example.invalid/", nil)
	if err != nil {
		t.Fatal(err)
	}
	return DebugTransport{Transport: fakeTransport{response: response}}.RoundTrip(req)
}

func TestRoundTripDoesNotBufferStreamingResponses(t *testing.T) {
	setNoColor(t, true)
	setBool(t, &logRequests, false)

	tests := []struct {
		name       string
		response   *http.Response
		wantNotice string
	}{
		{
			name: "protocol upgrade",
			response: &http.Response{
				StatusCode: http.StatusSwitchingProtocols,
				Status:     "101 Switching Protocols",
				Proto:      "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
				Header: http.Header{"Upgrade": {"websocket"}},
			},
			wantNotice: "[connection upgraded: body not captured]",
		},
		{
			name: "server-sent events",
			response: &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Proto:      "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
				Header: http.Header{"Content-Type": {"text/event-stream"}},
			},
			wantNotice: "[event stream: body not captured]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logged := captureLog(t)
			conn := &recordingConn{}
			tt.response.Body = conn

			got, err := roundTripCanned(t, tt.response)
			if err != nil {
				t.Fatalf("RoundTrip failed: %v", err)
			}
			if conn.readCalled {
				t.Error("body was buffered; it must be streamed straight to the client")
			}
			// httputil.ReverseProxy asserts the body of a 101 back to
			// io.ReadWriteCloser to splice the two connections together.
			if _, ok := got.Body.(io.ReadWriteCloser); !ok {
				t.Errorf("body type %T is not an io.ReadWriteCloser", got.Body)
			}
			// The headers are still logged even though the body is not.
			if out := logged.String(); !strings.Contains(out, tt.wantNotice) {
				t.Errorf("log missing %q, got:\n%s", tt.wantNotice, out)
			}
		})
	}
}

func TestRoundTripSkipsBodyWhenResponseLoggingDisabled(t *testing.T) {
	setNoColor(t, true)
	setBool(t, &logRequests, false)
	setBool(t, &logResponses, false)
	captureLog(t)

	conn := &recordingConn{}
	got, err := roundTripCanned(t, &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Proto:      "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header: http.Header{"Content-Type": {"application/json"}},
		Body:   conn,
	})
	if err != nil {
		t.Fatalf("RoundTrip failed: %v", err)
	}
	if conn.readCalled {
		t.Error("body was read even though response logging is disabled")
	}
	if got.Body != conn {
		t.Error("body was replaced even though response logging is disabled")
	}
}

func TestDecodeBodyCapsDecompressionBomb(t *testing.T) {
	// A small payload that expands to far more than the log limit.
	bomb := compressGzip(t, bytes.Repeat([]byte("A"), 64*maxLogBodySize))
	if len(bomb) > maxLogBodySize {
		t.Fatalf("test payload is not small: %d bytes", len(bomb))
	}

	got, err := decodeBody(encodingGzip, bomb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != maxLogBodySize+1 {
		t.Errorf("decoded %d bytes, want the read to stop at %d", len(got), maxLogBodySize+1)
	}
}

func TestDecodeBodyEncodingLists(t *testing.T) {
	original := []byte("hello, world!")

	tests := []struct {
		name     string
		encoding string
		body     []byte
		want     string
	}{
		{
			name:     "gzip alias",
			encoding: "x-gzip",
			body:     compressGzip(t, original),
			want:     "hello, world!",
		},
		{
			name:     "deflate alias",
			encoding: "x-deflate",
			body:     compressDeflate(t, original),
			want:     "hello, world!",
		},
		{
			// Codings are listed in the order they were applied, so brotli was
			// applied last and must be undone first.
			name:     "gzip then brotli",
			encoding: "gzip, br",
			body:     compressBrotli(t, compressGzip(t, original)),
			want:     "hello, world!",
		},
		{
			name:     "identity is a no-op in a list",
			encoding: "identity, gzip",
			body:     compressGzip(t, original),
			want:     "hello, world!",
		},
		{
			name:     "unsupported coding is left alone",
			encoding: "compress",
			body:     original,
			want:     "hello, world!",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeBody(tt.encoding, tt.body)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("got %q, want %q", string(got), tt.want)
			}
		})
	}
}

func TestFormatBodyForLog(t *testing.T) {
	setNoColor(t, true)
	original := []byte("hello, world!")

	tests := []struct {
		name        string
		body        []byte
		wireSize    int64
		encoding    string
		contentType string
		want        string
	}{
		{
			name:     "decode failure is reported instead of dumping raw bytes",
			body:     []byte("not gzip"),
			wireSize: 8,
			encoding: encodingGzip,
			want:     "[decode failed:",
		},
		{
			name:     "unsupported coding is reported",
			body:     original,
			wireSize: int64(len(original)),
			encoding: "compress",
			want:     "[not decoded: unsupported content-encoding",
		},
		{
			name:     "oversized plain body reports its exact size",
			body:     bytes.Repeat([]byte("x"), maxLogBodySize+1),
			wireSize: maxLogBodySize + 1,
			want:     fmt.Sprintf("[body too large to display: %d bytes]", maxLogBodySize+1),
		},
		{
			name:     "oversized body of unknown length",
			body:     bytes.Repeat([]byte("x"), maxLogBodySize+1),
			wireSize: -1,
			want:     fmt.Sprintf("[body too large to display: over %d bytes]", maxLogBodySize),
		},
		{
			name:     "oversized decompressed body",
			body:     compressGzip(t, bytes.Repeat([]byte("A"), 4*maxLogBodySize)),
			wireSize: 4 * maxLogBodySize,
			encoding: encodingGzip,
			want:     fmt.Sprintf("[decompressed body exceeds the %d byte log limit]", maxLogBodySize),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(formatBodyForLog(tt.body, tt.wireSize, tt.encoding, tt.contentType))
			if !strings.Contains(got, tt.want) {
				t.Errorf("got %q, want it to contain %q", got, tt.want)
			}
		})
	}
}

func TestRoundTripPreservesLargeRequestBody(t *testing.T) {
	// The request body is captured for logging only up to maxLogBodySize, so
	// the captured prefix must be spliced back in front of the remainder.
	setNoColor(t, true)
	captureLog(t)

	payload := bytes.Repeat([]byte("z"), maxLogBodySize+1024)

	var received []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	req, err := http.NewRequest(http.MethodPost, upstream.URL, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "text/plain")

	resp, err := DebugTransport{}.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if !bytes.Equal(received, payload) {
		t.Errorf("upstream received %d bytes, want %d (request body was corrupted)", len(received), len(payload))
	}
}

func TestResolveNoColor(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		env    string
		setEnv bool
		want   bool
	}{
		{name: "no flag, no env", args: nil, want: false},
		{name: "NO_COLOR set disables color", args: nil, env: "1", setEnv: true, want: true},
		{name: "empty NO_COLOR is ignored", args: nil, env: "", setEnv: true, want: false},
		{name: "explicit -no-color wins", args: []string{"-no-color"}, want: true},
		{
			// The project documents CLI flag > env var, so an explicit
			// -no-color=false keeps colors on even when NO_COLOR is set.
			name:   "explicit -no-color=false beats NO_COLOR",
			args:   []string{"-no-color=false"},
			env:    "1",
			setEnv: true,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv("NO_COLOR", tt.env)
			} else {
				_ = os.Unsetenv("NO_COLOR")
			}
			setNoColor(t, false)

			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			registerFlags(fs)
			if err := fs.Parse(tt.args); err != nil {
				t.Fatal(err)
			}

			if got := resolveNoColor(fs, noColor); got != tt.want {
				t.Errorf("resolveNoColor() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateListenPort(t *testing.T) {
	tests := []struct {
		name    string
		flag    string
		env     string
		setEnv  bool
		wantErr bool
	}{
		{name: "flag port", flag: "9090"},
		{name: "env port", env: "8080", setEnv: true},
		{name: "default when unset"},
		{name: "port zero picks a free port", flag: "0"},
		{name: "empty env is rejected", env: "", setEnv: true, wantErr: true},
		{name: "non-numeric is rejected", flag: "http", wantErr: true},
		{name: "out of range is rejected", flag: "70000", wantErr: true},
		{name: "host:port is rejected", flag: "0.0.0.0:8080", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setString(t, &cliPort, tt.flag)
			if tt.setEnv {
				t.Setenv("PORT", tt.env)
			} else {
				_ = os.Unsetenv("PORT")
			}

			err := validateListenPort()
			if tt.wantErr && err == nil {
				t.Error("expected an error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
