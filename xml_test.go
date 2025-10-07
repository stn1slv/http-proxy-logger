package main

import (
	"flag"
	"os"
	"strings"
	"testing"
)

func TestHighlightBodyXML(t *testing.T) {
	// Ensure colors are enabled for this test
	originalNoColor := noColor
	if noColor == nil {
		noColor = flag.Bool("no-color", false, "disable colored output")
	}
	defer func() { noColor = originalNoColor }()
	*noColor = false

	data := []byte(`<p>Hello</p>`)
	out := string(highlightBody(data, "application/xml"))
	if out == string(data) {
		t.Fatalf("expected XML body to be highlighted")
	}
	if !strings.Contains(out, colorTag) {
		t.Errorf("highlighted XML missing tag color: %q", out)
	}
}

func TestXMLNamespacePreservation(t *testing.T) {
	// Reset flags
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	noColor = flag.Bool("no-color", true, "disable colored output")

	soapBody := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/" xmlns:xsd="http://www.w3.org/2001/XMLSchema" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
<soapenv:Body>
<test>value</test>
</soapenv:Body>
</soapenv:Envelope>`)

	result := highlightBody(soapBody, "text/xml")
	resultStr := string(result)

	// Check that namespace prefixes are preserved
	if !strings.Contains(resultStr, "soapenv:Envelope") {
		t.Errorf("Expected 'soapenv:Envelope' to be preserved in highlighted XML, got:\n%s", resultStr)
	}

	if !strings.Contains(resultStr, "soapenv:Body") {
		t.Errorf("Expected 'soapenv:Body' to be preserved in highlighted XML, got:\n%s", resultStr)
	}

	// Check that namespace declarations are preserved
	if !strings.Contains(resultStr, "xmlns:soapenv") {
		t.Errorf("Expected 'xmlns:soapenv' namespace declaration to be preserved, got:\n%s", resultStr)
	}

	if !strings.Contains(resultStr, "http://schemas.xmlsoap.org/soap/envelope/") {
		t.Errorf("Expected namespace URI to be preserved, got:\n%s", resultStr)
	}

	// Check that XML declaration is preserved
	if !strings.Contains(resultStr, "<?xml") {
		t.Errorf("Expected XML declaration to be preserved, got:\n%s", resultStr)
	}

	t.Logf("✓ XML namespaces and prefixes are preserved in highlighting:\n%s", resultStr)
}

func TestXMLMultipleNamespaces(t *testing.T) {
	// Reset flags
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	noColor = flag.Bool("no-color", true, "disable colored output")

	xmlBody := []byte(`<?xml version="1.0"?>
<root xmlns:a="http://example.com/a" xmlns:b="http://example.com/b">
<a:element1>value1</a:element1>
<b:element2 b:attr="test">value2</b:element2>
</root>`)

	result := highlightBody(xmlBody, "application/xml")
	resultStr := string(result)

	// Check multiple namespace prefixes
	if !strings.Contains(resultStr, "a:element1") {
		t.Errorf("Expected 'a:element1' to be preserved, got:\n%s", resultStr)
	}

	if !strings.Contains(resultStr, "b:element2") {
		t.Errorf("Expected 'b:element2' to be preserved, got:\n%s", resultStr)
	}

	if !strings.Contains(resultStr, "b:attr") {
		t.Errorf("Expected 'b:attr' attribute with namespace to be preserved, got:\n%s", resultStr)
	}

	// Check namespace declarations
	if !strings.Contains(resultStr, "xmlns:a") {
		t.Errorf("Expected 'xmlns:a' declaration to be preserved, got:\n%s", resultStr)
	}

	if !strings.Contains(resultStr, "xmlns:b") {
		t.Errorf("Expected 'xmlns:b' declaration to be preserved, got:\n%s", resultStr)
	}

	t.Logf("✓ Multiple XML namespaces are correctly preserved:\n%s", resultStr)
}

func TestXMLDefaultNamespace(t *testing.T) {
	// Reset flags
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	noColor = flag.Bool("no-color", true, "disable colored output")

	xmlBody := []byte(`<?xml version="1.0"?>
<root xmlns="http://example.com/default">
<child>value</child>
</root>`)

	result := highlightBody(xmlBody, "text/xml")
	resultStr := string(result)

	// Check that default namespace is preserved
	if !strings.Contains(resultStr, "xmlns=") {
		t.Errorf("Expected default namespace 'xmlns=' to be preserved, got:\n%s", resultStr)
	}

	if !strings.Contains(resultStr, "http://example.com/default") {
		t.Errorf("Expected default namespace URI to be preserved, got:\n%s", resultStr)
	}

	t.Logf("✓ Default XML namespace is correctly preserved:\n%s", resultStr)
}

func TestHighlightXMLWithComments(t *testing.T) {
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	noColor = flag.Bool("no-color", true, "disable colored output")

	xmlBody := []byte(`<?xml version="1.0"?>
<root>
<!-- This is a comment -->
<child>value</child>
</root>`)

	result := highlightBody(xmlBody, "text/xml")
	resultStr := string(result)

	if !strings.Contains(resultStr, "<!-- This is a comment -->") {
		t.Errorf("Expected XML comment to be preserved, got:\n%s", resultStr)
	}
}

func TestHighlightXMLInvalid(t *testing.T) {
	data := []byte(`<invalid><xml>`)
	result := highlightXML(data)
	// Should return original data when XML is invalid
	if result != string(data) {
		t.Errorf("Invalid XML should be returned as-is, got: %q", result)
	}
}
