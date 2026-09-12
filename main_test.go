package main

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

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

// guardFlagVars restores every flag-backed variable after the test.
// registerFlags writes each default straight into its target, so all of them
// must be guarded, not just the one under test.
func guardFlagVars(t *testing.T) {
	t.Helper()
	setNoColor(t, noColor)
	setBool(t, &logRequests, logRequests)
	setBool(t, &logResponses, logResponses)
	setBool(t, &showVersion, showVersion)
	setString(t, &cliTarget, cliTarget)
	setString(t, &cliPort, cliPort)
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

// fakeTransport returns a canned response, or err when set, instead of
// contacting a server.
type fakeTransport struct {
	response *http.Response
	err      error
}

func (f fakeTransport) RoundTrip(*http.Request) (*http.Response, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.response, nil
}

// recordingConn is a response body that records how it was used. It implements
// io.ReadWriteCloser so it can stand in for a hijacked connection.
type recordingConn struct {
	readCalled bool
	closeCount int
}

func (c *recordingConn) Read([]byte) (int, error)    { c.readCalled = true; return 0, io.EOF }
func (c *recordingConn) Write(p []byte) (int, error) { return len(p), nil }
func (c *recordingConn) Close() error                { c.closeCount++; return nil }

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
	// The caller still owns the body here, so RoundTrip must not have closed it.
	if conn.closeCount != 0 {
		t.Errorf("body closed %d times; it is being handed back to the caller", conn.closeCount)
	}
}

func TestRoundTripLogsUpstreamError(t *testing.T) {
	upstreamErr := errors.New("dial tcp 127.0.0.1:1: connection refused")

	for _, logResp := range []bool{true, false} {
		t.Run(fmt.Sprintf("responses=%v", logResp), func(t *testing.T) {
			setNoColor(t, true)
			setBool(t, &logRequests, false)
			setBool(t, &logResponses, logResp)
			logged := captureLog(t)

			req, err := http.NewRequest(http.MethodGet, "http://example.invalid/", nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = DebugTransport{Transport: fakeTransport{err: upstreamErr}}.RoundTrip(req)
			if !errors.Is(err, upstreamErr) {
				t.Fatalf("RoundTrip error = %v, want %v", err, upstreamErr)
			}

			out := logged.String()
			if !logResp {
				if out != "" {
					t.Errorf("logged output with -responses=false:\n%s", out)
				}
				return
			}
			pattern := regexp.MustCompile(`--- RESPONSE \d+ \(upstream error: ` + regexp.QuoteMeta(upstreamErr.Error()) + `, \S+\) ---`)
			if !pattern.MatchString(out) {
				t.Errorf("log does not match %q, got:\n%s", pattern, out)
			}
		})
	}
}

func TestRoundTripLogsLatency(t *testing.T) {
	setNoColor(t, true)
	setBool(t, &logRequests, false)
	setBool(t, &logResponses, true)
	logged := captureLog(t)

	_, err := roundTripCanned(t, &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Proto:      "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header: http.Header{},
		Body:   io.NopCloser(strings.NewReader("ok")),
	})
	if err != nil {
		t.Fatalf("RoundTrip failed: %v", err)
	}
	pattern := regexp.MustCompile(`--- RESPONSE \d+ \(200 OK, [0-9.]+[a-zµ]*s\) ---`)
	if out := logged.String(); !pattern.MatchString(out) {
		t.Errorf("log does not match %q, got:\n%s", pattern, out)
	}
}

func TestFormatElapsed(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{in: 0, want: "0s"},
		{in: 412345 * time.Nanosecond, want: "412µs"},
		{in: 134567 * time.Microsecond, want: "135ms"},
		{in: 2345 * time.Millisecond, want: "2.345s"},
	}
	for _, tt := range tests {
		if got := formatElapsed(tt.in); got != tt.want {
			t.Errorf("formatElapsed(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestRegisterFlagsVersion(t *testing.T) {
	for _, arg := range []string{"-v", "--v", "-version", "--version"} {
		t.Run(arg, func(t *testing.T) {
			guardFlagVars(t)

			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			registerFlags(fs)
			if showVersion {
				t.Fatal("showVersion is true before parsing")
			}
			if err := fs.Parse([]string{arg}); err != nil {
				t.Fatal(err)
			}
			if !showVersion {
				t.Errorf("%s did not set showVersion", arg)
			}
		})
	}
}

func TestRoundTripClosesUpstreamBody(t *testing.T) {
	// Buffering the body for logging leaves the upstream body owned by
	// RoundTrip, which must close it exactly once so the connection is released.
	setNoColor(t, true)
	setBool(t, &logRequests, false)
	setBool(t, &logResponses, true)
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
	if conn.closeCount != 1 {
		t.Errorf("upstream body closed %d times, want exactly 1", conn.closeCount)
	}
	// The client must still get a usable body, and closing it must not reach
	// the upstream body a second time.
	if err := got.Body.Close(); err != nil {
		t.Errorf("closing the returned body failed: %v", err)
	}
	if conn.closeCount != 1 {
		t.Errorf("upstream body closed %d times after the client closed its own", conn.closeCount)
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

func TestFormatBodyForLogPutsNoticeBeforeBody(t *testing.T) {
	// An undecodable body is still shown, but the explanation has to come
	// first: the raw bytes may be long or binary and would otherwise push the
	// notice out of view.
	setNoColor(t, true)

	body := []byte("this is not gzip at all")
	got := string(formatBodyForLog(body, int64(len(body)), encodingGzip, "text/plain"))

	notice := strings.Index(got, "[decode failed:")
	payload := strings.Index(got, string(body))
	if notice < 0 || payload < 0 {
		t.Fatalf("expected both the notice and the raw body, got %q", got)
	}
	if notice > payload {
		t.Errorf("notice must precede the body, got %q", got)
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
			guardFlagVars(t)

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

func TestDumpRequestHeadersDoesNotMaterializeDeclaredBody(t *testing.T) {
	// httputil.DumpRequestOut writes a dummy body of ContentLength bytes into
	// its buffer before slicing it off, so dumping the request directly would
	// let a client allocate gigabytes by declaring a Content-Length it never
	// sends. The dump must stay proportional to the headers.
	const declared = 1 << 30 // 1 GiB

	req, err := http.NewRequest(http.MethodPost, "http://example.invalid/upload", strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "text/plain")
	req.ContentLength = declared

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	dump, err := dumpRequestHeaders(req)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatalf("dumpRequestHeaders failed: %v", err)
	}

	if len(dump) > 4096 {
		t.Errorf("header dump is %d bytes; the declared body must not be materialized", len(dump))
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 1<<20 {
		t.Errorf("dumping allocated %d bytes for a %d byte declared body", allocated, declared)
	}
	// The real length must still be reported, not the empty copy's.
	if want := fmt.Sprintf("Content-Length: %d\r\n", declared); !strings.Contains(string(dump), want) {
		t.Errorf("dump does not report the real length %q, got:\n%s", want, dump)
	}
}

func TestDumpRequestHeadersReportsFraming(t *testing.T) {
	tests := []struct {
		name          string
		contentLength int64
		body          string
		want          string
		notWant       string
	}{
		{name: "known length", contentLength: 15, body: "0123456789abcde", want: "Content-Length: 15\r\n"},
		{name: "no body", contentLength: 0, body: "", notWant: "Content-Length: 1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, "http://example.invalid/", strings.NewReader(tt.body))
			if err != nil {
				t.Fatal(err)
			}
			req.ContentLength = tt.contentLength

			dump, err := dumpRequestHeaders(req)
			if err != nil {
				t.Fatalf("dumpRequestHeaders failed: %v", err)
			}
			got := string(dump)
			if tt.want != "" && !strings.Contains(got, tt.want) {
				t.Errorf("dump missing %q, got:\n%s", tt.want, got)
			}
			if tt.notWant != "" && strings.Contains(got, tt.notWant) {
				t.Errorf("dump unexpectedly contains %q, got:\n%s", tt.notWant, got)
			}
			// The request body must be left intact for the transport to send.
			sent, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatal(err)
			}
			if string(sent) != tt.body {
				t.Errorf("request body was consumed: got %q, want %q", sent, tt.body)
			}
		})
	}
}

func TestHighlightersBoundIndentation(t *testing.T) {
	// Indentation is written per token, so an uncapped indent makes output
	// quadratic in nesting depth: a deeply nested body at the log limit would
	// expand to tens of gigabytes.
	setNoColor(t, true)

	tests := []struct {
		name        string
		body        string
		contentType string
	}{
		{
			name:        "xml",
			body:        strings.Repeat("<a>", 16000) + strings.Repeat("</a>", 16000),
			contentType: "application/xml",
		},
		{
			name:        "json",
			body:        strings.Repeat("[", 5000) + strings.Repeat("]", 5000),
			contentType: "application/json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := []byte(tt.body)
			out := highlightBody(in, tt.contentType)
			// With the cap, output stays linear in input: every token gains at
			// most maxIndentDepth*2 spaces plus a newline.
			limit := len(in) * (2*maxIndentDepth + 8)
			if len(out) > limit {
				t.Errorf("highlighting %d bytes produced %d bytes (limit %d): indentation is not bounded",
					len(in), len(out), limit)
			}
		})
	}
}
