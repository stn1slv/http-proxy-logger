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

func TestXMLInlineTextFormatting(t *testing.T) {
	// Reset flags
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	noColor = flag.Bool("no-color", true, "disable colored output")

	// Test case: SOAP request with simple text elements
	soapRequest := []byte(`<soap:Envelope xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xmlns:xsd="http://www.w3.org/2001/XMLSchema" xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <Subtract xmlns="http://tempuri.org/">
      <intA>15</intA>
      <intB>5</intB>
    </Subtract>
  </soap:Body>
</soap:Envelope>`)

	result := highlightBody(soapRequest, "text/xml")
	resultStr := string(result)

	// Check that simple text elements are formatted inline (not on separate lines)
	// The pattern should be <intA>...text...</intA> on one line
	// We use regex/contains to account for whitespace in the original XML

	// Check that intA element contains inline text (no newline between opening and closing tag)
	if !strings.Contains(resultStr, "<intA>15") && !strings.Contains(resultStr, "</intA>") {
		t.Errorf("Expected '<intA>' element to contain '15' inline, got:\n%s", resultStr)
	}

	if !strings.Contains(resultStr, "<intB>5") && !strings.Contains(resultStr, "</intB>") {
		t.Errorf("Expected '<intB>' element to contain '5' inline, got:\n%s", resultStr)
	}

	// Verify that the elements are truly inline by checking there's no newline between tag and text
	// Count lines - if formatted correctly, intA and intB should each be on single lines
	lines := strings.Split(resultStr, "\n")
	foundIntAInline := false
	foundIntBInline := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "<intA>") && strings.HasSuffix(trimmed, "</intA>") {
			foundIntAInline = true
		}
		if strings.HasPrefix(trimmed, "<intB>") && strings.HasSuffix(trimmed, "</intB>") {
			foundIntBInline = true
		}
	}

	if !foundIntAInline {
		t.Errorf("Expected '<intA>' element with text to be on a single line, got:\n%s", resultStr)
	}

	if !foundIntBInline {
		t.Errorf("Expected '<intB>' element with text to be on a single line, got:\n%s", resultStr)
	}

	// Check that parent elements with nested children still have proper line breaks
	// We check that <soap:Body> is followed by a newline, not inline with its closing tag
	foundBodyWithNewline := false
	for i, line := range lines {
		if strings.Contains(line, "<soap:Body>") {
			// If Body is on its own line (not followed immediately by </soap:Body> on same line)
			if !strings.Contains(line, "</soap:Body>") {
				// And if there's a next line with content
				if i+1 < len(lines) && strings.TrimSpace(lines[i+1]) != "" {
					foundBodyWithNewline = true
					break
				}
			}
		}
	}
	if !foundBodyWithNewline {
		t.Errorf("Expected '<soap:Body>' to be followed by content on a new line (has nested children), got:\n%s", resultStr)
	}

	// Verify namespace preservation
	if !strings.Contains(resultStr, "soap:Envelope") {
		t.Errorf("Expected 'soap:Envelope' namespace prefix to be preserved, got:\n%s", resultStr)
	}

	t.Logf("✓ XML inline text formatting is working correctly:\n%s", resultStr)
}

func TestXMLMixedContent(t *testing.T) {
	// Reset flags
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	noColor = flag.Bool("no-color", true, "disable colored output")

	// Test various element types: simple text, nested elements, empty elements
	xmlBody := []byte(`<?xml version="1.0"?>
<root>
  <simpleText>Hello World</simpleText>
  <parent>
    <child1>Value 1</child1>
    <child2>Value 2</child2>
  </parent>
  <number>42</number>
</root>`)

	result := highlightBody(xmlBody, "application/xml")
	resultStr := string(result)

	// Check inline formatting by verifying elements are on single lines
	lines := strings.Split(resultStr, "\n")

	foundSimpleTextInline := false
	foundChild1Inline := false
	foundChild2Inline := false
	foundNumberInline := false
	foundParentWithNewline := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "<simpleText>") && strings.HasSuffix(trimmed, "</simpleText>") {
			foundSimpleTextInline = true
		}
		if strings.HasPrefix(trimmed, "<child1>") && strings.HasSuffix(trimmed, "</child1>") {
			foundChild1Inline = true
		}
		if strings.HasPrefix(trimmed, "<child2>") && strings.HasSuffix(trimmed, "</child2>") {
			foundChild2Inline = true
		}
		if strings.HasPrefix(trimmed, "<number>") && strings.HasSuffix(trimmed, "</number>") {
			foundNumberInline = true
		}
		if strings.Contains(trimmed, "<parent>") && i+1 < len(lines) {
			// Parent should have newline after it (not inline with closing tag)
			nextLine := strings.TrimSpace(lines[i+1])
			if strings.HasPrefix(nextLine, "<child1>") {
				foundParentWithNewline = true
			}
		}
	}

	if !foundSimpleTextInline {
		t.Errorf("Expected '<simpleText>' with text to be on single line, got:\n%s", resultStr)
	}

	if !foundChild1Inline {
		t.Errorf("Expected '<child1>' with text to be on single line, got:\n%s", resultStr)
	}

	if !foundChild2Inline {
		t.Errorf("Expected '<child2>' with text to be on single line, got:\n%s", resultStr)
	}

	if !foundNumberInline {
		t.Errorf("Expected '<number>' with text to be on single line, got:\n%s", resultStr)
	}

	if !foundParentWithNewline {
		t.Errorf("Expected '<parent>' to be followed by newline (has nested children), got:\n%s", resultStr)
	}

	t.Logf("✓ XML mixed content formatting is working correctly:\n%s", resultStr)
}
