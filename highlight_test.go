package main

import (
	"flag"
	"strings"
	"testing"
	"time"
)

// Tests for header highlighting

func TestHighlightHeadersRequest(t *testing.T) {
	headers := []byte("POST /foo HTTP/1.1\r\nHost: example.com\r\n\r\n")
	out := string(highlightHeaders(headers, true))
	if !strings.Contains(out, colorMethod) || !strings.Contains(out, colorURL) {
		t.Errorf("request line not highlighted: %q", out)
	}
	if !strings.Contains(out, colorHeader) {
		t.Errorf("header key not highlighted: %q", out)
	}
}

func TestHighlightHeadersResponse(t *testing.T) {
	headers := []byte("HTTP/1.1 404 Not Found\r\nContent-Type: text/plain\r\n\r\n")
	out := string(highlightHeaders(headers, false))
	if !strings.Contains(out, colorStatus4xx) {
		t.Errorf("status not colorized: %q", out)
	}
	if !strings.Contains(out, colorHeader) {
		t.Errorf("header key not highlighted: %q", out)
	}
}

func TestHighlightHeadersWithColorsDisabled(t *testing.T) {
	// Save original state
	originalNoColor := noColor
	if noColor == nil {
		noColor = flag.Bool("no-color", false, "disable colored output")
	}
	defer func() { noColor = originalNoColor }()

	*noColor = true

	headers := []byte("POST /foo HTTP/1.1\r\nHost: example.com\r\n\r\n")
	out := string(highlightHeaders(headers, true))

	if strings.Contains(out, colorMethod) || strings.Contains(out, colorURL) || strings.Contains(out, colorHeader) {
		t.Errorf("headers with no-color should not contain color codes: %q", out)
	}
	// Should still contain the actual header content
	if !strings.Contains(out, "POST /foo HTTP/1.1") || !strings.Contains(out, "Host: example.com") {
		t.Errorf("headers should still contain the data: %q", out)
	}
}

// Tests for status code coloring

func TestColorStatus(t *testing.T) {
	if colorStatus(201) != colorStatus2xx {
		t.Errorf("expected 2xx color")
	}
	if colorStatus(302) != colorStatus3xx {
		t.Errorf("expected 3xx color")
	}
	if colorStatus(404) != colorStatus4xx {
		t.Errorf("expected 4xx color")
	}
	if colorStatus(500) != colorStatus5xx {
		t.Errorf("expected 5xx color")
	}
}

// Tests for color wrapping utility

func TestWrapColorWithColorsEnabled(t *testing.T) {
	// Save original state
	originalNoColor := noColor
	if noColor == nil {
		// Initialize flag if not already done
		noColor = flag.Bool("no-color", false, "disable colored output")
	}
	defer func() { noColor = originalNoColor }()

	// Set no-color to false (colors enabled)
	*noColor = false

	result := wrapColor("test", colorString)
	expected := colorString + "test" + colorReset
	if result != expected {
		t.Errorf("wrapColor with colors enabled: got %q, want %q", result, expected)
	}
}

func TestWrapColorWithColorsDisabled(t *testing.T) {
	// Save original state
	originalNoColor := noColor
	if noColor == nil {
		// Initialize flag if not already done
		noColor = flag.Bool("no-color", false, "disable colored output")
	}
	defer func() { noColor = originalNoColor }()

	// Set no-color to true (colors disabled)
	*noColor = true

	result := wrapColor("test", colorString)
	expected := "test"
	if result != expected {
		t.Errorf("wrapColor with colors disabled: got %q, want %q", result, expected)
	}
}

// Tests for time formatting

func TestColoredTimeWithColorsEnabled(t *testing.T) {
	// Save original state
	originalNoColor := noColor
	if noColor == nil {
		noColor = flag.Bool("no-color", false, "disable colored output")
	}
	defer func() { noColor = originalNoColor }()

	*noColor = false

	testTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	result := coloredTime(testTime)

	if !strings.Contains(result, colorTime) {
		t.Errorf("coloredTime with colors enabled should contain color codes: %q", result)
	}
	if !strings.Contains(result, "[2023/01/01 12:00:00]") {
		t.Errorf("coloredTime should contain formatted time: %q", result)
	}
}

func TestColoredTimeWithColorsDisabled(t *testing.T) {
	// Save original state
	originalNoColor := noColor
	if noColor == nil {
		noColor = flag.Bool("no-color", false, "disable colored output")
	}
	defer func() { noColor = originalNoColor }()

	*noColor = true

	testTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	result := coloredTime(testTime)
	expected := "[2023/01/01 12:00:00]"

	if result != expected {
		t.Errorf("coloredTime with colors disabled: got %q, want %q", result, expected)
	}
	if strings.Contains(result, colorTime) {
		t.Errorf("coloredTime with colors disabled should not contain color codes: %q", result)
	}
}

func TestColoredTimeWithColorWithColorsEnabled(t *testing.T) {
	// Save original state
	originalNoColor := noColor
	if noColor == nil {
		noColor = flag.Bool("no-color", false, "disable colored output")
	}
	defer func() { noColor = originalNoColor }()

	*noColor = false

	testTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	result := coloredTimeWithColor(testTime, colorReqMarker)

	if !strings.Contains(result, colorReqMarker) {
		t.Errorf("coloredTimeWithColor with colors enabled should contain color codes: %q", result)
	}
	if !strings.Contains(result, "[2023/01/01 12:00:00]") {
		t.Errorf("coloredTimeWithColor should contain formatted time: %q", result)
	}
}

func TestColoredTimeWithColorWithColorsDisabled(t *testing.T) {
	// Save original state
	originalNoColor := noColor
	if noColor == nil {
		noColor = flag.Bool("no-color", false, "disable colored output")
	}
	defer func() { noColor = originalNoColor }()

	*noColor = true

	testTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	result := coloredTimeWithColor(testTime, colorReqMarker)
	expected := "[2023/01/01 12:00:00]"

	if result != expected {
		t.Errorf("coloredTimeWithColor with colors disabled: got %q, want %q", result, expected)
	}
	if strings.Contains(result, colorReqMarker) {
		t.Errorf("coloredTimeWithColor with colors disabled should not contain color codes: %q", result)
	}
}
