package main

import (
	"flag"
	"strings"
	"testing"
)

func TestHighlightBodyJSON(t *testing.T) {
	// Ensure colors are enabled for this test
	originalNoColor := noColor
	if noColor == nil {
		noColor = flag.Bool("no-color", false, "disable colored output")
	}
	defer func() { noColor = originalNoColor }()
	*noColor = false

	data := []byte(`{"name":"Alice"}`)
	out := string(highlightBody(data, "application/json"))
	if out == string(data) {
		t.Fatalf("expected JSON body to be highlighted")
	}
	if !strings.Contains(out, colorKey) || !strings.Contains(out, colorString) {
		t.Errorf("highlighted JSON missing colors: %q", out)
	}
}

func TestHighlightBodyJSONWithColorsDisabled(t *testing.T) {
	// Save original state
	originalNoColor := noColor
	if noColor == nil {
		noColor = flag.Bool("no-color", false, "disable colored output")
	}
	defer func() { noColor = originalNoColor }()

	*noColor = true

	data := []byte(`{"name":"Alice"}`)
	out := string(highlightBody(data, "application/json"))

	// Should still format JSON but without colors
	if strings.Contains(out, colorKey) || strings.Contains(out, colorString) {
		t.Errorf("highlighted JSON with no-color should not contain color codes: %q", out)
	}
	// Should still contain the formatted structure
	if !strings.Contains(out, `"name"`) || !strings.Contains(out, `"Alice"`) {
		t.Errorf("highlighted JSON should still contain the data: %q", out)
	}
}

func TestHighlightJSONValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string
	}{
		{
			name:     "simple object",
			input:    `{"key":"value"}`,
			contains: []string{`"key"`, `"value"`},
		},
		{
			name:     "nested object",
			input:    `{"outer":{"inner":"value"}}`,
			contains: []string{`"outer"`, `"inner"`, `"value"`},
		},
		{
			name:     "array",
			input:    `{"items":[1,2,3]}`,
			contains: []string{`"items"`, "1", "2", "3"},
		},
		{
			name:     "boolean and null",
			input:    `{"bool":true,"null":null}`,
			contains: []string{`"bool"`, "true", `"null"`, "null"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := highlightJSON([]byte(tt.input))
			for _, expected := range tt.contains {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected %q to contain %q", result, expected)
				}
			}
		})
	}
}

func TestHighlightJSONInvalid(t *testing.T) {
	data := []byte(`{invalid json}`)
	result := highlightJSON(data)
	// Should return original data when JSON is invalid
	if result != string(data) {
		t.Errorf("Invalid JSON should be returned as-is, got: %q", result)
	}
}
