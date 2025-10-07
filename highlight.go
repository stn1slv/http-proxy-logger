package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	colorReset     = "\033[0m"
	colorKey       = "\033[36m"
	colorString    = "\033[32m"
	colorNumber    = "\033[33m"
	colorBool      = "\033[35m"
	colorNull      = "\033[90m"
	colorPunct     = "\033[37m"
	colorTag       = "\033[34m"
	colorAttr      = "\033[33m"
	colorMethod    = "\033[35m"
	colorURL       = "\033[36m"
	colorHeader    = "\033[34m"
	colorStatus2xx = "\033[32m"
	colorStatus3xx = "\033[36m"
	colorStatus4xx = "\033[33m"
	colorStatus5xx = "\033[31m"
	colorReqMarker = "\033[33m"
	colorResMarker = "\033[95m"
	colorTime      = "\033[90m"
)

func wrapColor(s, color string) string {
	if *noColor {
		return s
	}
	return color + s + colorReset
}

func coloredTime(t time.Time) string {
	if *noColor {
		return "[" + t.Format("2006/01/02 15:04:05") + "]"
	}
	return wrapColor("["+t.Format("2006/01/02 15:04:05")+"]", colorTime)
}

func highlightJSONValue(v interface{}, indent int) string {
	switch t := v.(type) {
	case map[string]interface{}:
		var keys []string
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		b.WriteString(colorPunct + "{" + colorReset + "\n")
		indent++
		for i, k := range keys {
			b.WriteString(strings.Repeat("  ", indent))
			b.WriteString(wrapColor("\""+k+"\"", colorKey))
			b.WriteString(wrapColor(": ", colorPunct))
			b.WriteString(highlightJSONValue(t[k], indent))
			if i < len(keys)-1 {
				b.WriteString(wrapColor(",", colorPunct))
			}
			b.WriteString("\n")
		}
		indent--
		b.WriteString(strings.Repeat("  ", indent))
		b.WriteString(colorPunct + "}" + colorReset)
		return b.String()
	case []interface{}:
		var b strings.Builder
		b.WriteString(colorPunct + "[" + colorReset + "\n")
		indent++
		for i, val := range t {
			b.WriteString(strings.Repeat("  ", indent))
			b.WriteString(highlightJSONValue(val, indent))
			if i < len(t)-1 {
				b.WriteString(wrapColor(",", colorPunct))
			}
			b.WriteString("\n")
		}
		indent--
		b.WriteString(strings.Repeat("  ", indent))
		b.WriteString(colorPunct + "]" + colorReset)
		return b.String()
	case string:
		return wrapColor(strconv.Quote(t), colorString)
	case float64:
		return wrapColor(strconv.FormatFloat(t, 'f', -1, 64), colorNumber)
	case bool:
		return wrapColor(strconv.FormatBool(t), colorBool)
	default:
		return wrapColor("null", colorNull)
	}
}

func highlightJSON(data []byte) string {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return string(data)
	}
	return highlightJSONValue(v, 0)
}

func highlightXML(data []byte) string {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var b strings.Builder
	indent := 0

	// We need to track namespace prefixes as we encounter them
	nsPrefixes := make(map[string]string) // maps namespace URL to prefix

	tokens := []xml.Token{}
	// First pass: collect all tokens
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return string(data)
		}
		tokens = append(tokens, xml.CopyToken(tok))
	}

	justWroteStartTag := false
	justWroteInlineText := false

	for i := 0; i < len(tokens); i++ {
		switch tok := tokens[i].(type) {
		case xml.StartElement:
			// Check for namespace declarations in attributes
			var nsAttrs []xml.Attr
			var regularAttrs []xml.Attr

			for _, attr := range tok.Attr {
				if attr.Name.Space == "xmlns" {
					// This is a namespace declaration with prefix: xmlns:prefix="uri"
					nsPrefixes[attr.Value] = attr.Name.Local
					nsAttrs = append(nsAttrs, attr)
				} else if attr.Name.Local == "xmlns" && attr.Name.Space == "" {
					// This is the default namespace: xmlns="uri"
					nsPrefixes[attr.Value] = ""
					nsAttrs = append(nsAttrs, attr)
				} else {
					regularAttrs = append(regularAttrs, attr)
				}
			}

			// Determine the element's full name with prefix
			elementName := tok.Name.Local
			if tok.Name.Space != "" {
				if prefix, ok := nsPrefixes[tok.Name.Space]; ok && prefix != "" {
					elementName = prefix + ":" + tok.Name.Local
				}
			}

			// Check if next token is simple text (not another element)
			hasSimpleText := false
			if i+1 < len(tokens) {
				if charData, ok := tokens[i+1].(xml.CharData); ok {
					txt := strings.TrimSpace(string(charData))
					if txt != "" {
						// Check if the token after CharData is EndElement
						// (or if there's whitespace CharData, check after that)
						nextEndIdx := i + 2
						for nextEndIdx < len(tokens) {
							if _, ok := tokens[nextEndIdx].(xml.CharData); ok {
								// Skip whitespace-only CharData
								if strings.TrimSpace(string(tokens[nextEndIdx].(xml.CharData))) == "" {
									nextEndIdx++
									continue
								}
								break
							}
							if _, isEndElement := tokens[nextEndIdx].(xml.EndElement); isEndElement {
								hasSimpleText = true
							}
							break
						}
					}
				}
			}

			if !justWroteStartTag {
				b.WriteString(strings.Repeat("  ", indent))
			}
			b.WriteString(wrapColor("<"+elementName, colorTag))

			// Write namespace declarations first
			for _, attr := range nsAttrs {
				b.WriteString(" ")
				if attr.Name.Space == "xmlns" {
					b.WriteString(wrapColor("xmlns:"+attr.Name.Local, colorAttr))
				} else {
					b.WriteString(wrapColor("xmlns", colorAttr))
				}
				b.WriteString(wrapColor("=", colorPunct))
				b.WriteString(wrapColor("\""+attr.Value+"\"", colorString))
			}

			// Write regular attributes
			for _, attr := range regularAttrs {
				b.WriteString(" ")
				attrName := attr.Name.Local
				if attr.Name.Space != "" {
					if prefix, ok := nsPrefixes[attr.Name.Space]; ok && prefix != "" {
						attrName = prefix + ":" + attr.Name.Local
					}
				}
				b.WriteString(wrapColor(attrName, colorAttr))
				b.WriteString(wrapColor("=", colorPunct))
				b.WriteString(wrapColor("\""+attr.Value+"\"", colorString))
			}
			b.WriteString(wrapColor(">", colorTag))

			if !hasSimpleText {
				b.WriteString("\n")
			}

			indent++
			justWroteStartTag = hasSimpleText

		case xml.EndElement:
			indent--
			// Determine the element's full name with prefix for end tag
			elementName := tok.Name.Local
			if tok.Name.Space != "" {
				if prefix, ok := nsPrefixes[tok.Name.Space]; ok && prefix != "" {
					elementName = prefix + ":" + tok.Name.Local
				}
			}
			if !justWroteStartTag && !justWroteInlineText {
				b.WriteString(strings.Repeat("  ", indent))
			}
			b.WriteString(wrapColor("</"+elementName+">", colorTag))
			b.WriteString("\n")
			justWroteStartTag = false
			justWroteInlineText = false

		case xml.CharData:
			txt := strings.TrimSpace(string([]byte(tok)))
			if len(txt) > 0 {
				if justWroteStartTag {
					// Keep text on same line as opening tag
					written := wrapColor(txt, colorString)
					b.WriteString(written)
					justWroteStartTag = false
					justWroteInlineText = true
				} else {
					// Multi-line or separate text content
					b.WriteString(strings.Repeat("  ", indent))
					b.WriteString(wrapColor(txt, colorString))
					b.WriteString("\n")
					justWroteStartTag = false
					justWroteInlineText = false
				}
			}
			// Note: We skip whitespace-only CharData tokens by not writing anything

		case xml.Comment:
			if !justWroteStartTag {
				b.WriteString(strings.Repeat("  ", indent))
			}
			b.WriteString(wrapColor("<!--"+string(tok)+"-->", colorNull))
			b.WriteString("\n")
			justWroteStartTag = false
			justWroteInlineText = false

		case xml.ProcInst:
			// Handle XML declarations like <?xml version="1.0"?>
			b.WriteString(wrapColor("<?"+tok.Target+" "+string(tok.Inst)+"?>", colorNull))
			b.WriteString("\n")
			justWroteStartTag = false
			justWroteInlineText = false
		}
	}
	return b.String()
}

func highlightBody(data []byte, contentType string) []byte {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "json") {
		return []byte(highlightJSON(data))
	}
	if strings.Contains(ct, "xml") {
		return []byte(highlightXML(data))
	}
	return data
}

func colorStatus(code int) string {
	switch {
	case code >= 200 && code < 300:
		return colorStatus2xx
	case code >= 300 && code < 400:
		return colorStatus3xx
	case code >= 400 && code < 500:
		return colorStatus4xx
	default:
		return colorStatus5xx
	}
}

func highlightHeaders(data []byte, isRequest bool) []byte {
	lines := strings.Split(string(bytes.TrimSuffix(data, []byte("\r\n"))), "\r\n")
	if len(lines) == 0 {
		return data
	}

	if isRequest {
		parts := strings.SplitN(lines[0], " ", 3)
		if len(parts) == 3 {
			lines[0] = wrapColor(parts[0], colorMethod) + " " + wrapColor(parts[1], colorURL) + " " + parts[2]
		}
	} else {
		parts := strings.SplitN(lines[0], " ", 3)
		if len(parts) >= 2 {
			code, _ := strconv.Atoi(parts[1])
			status := strings.Join(parts[1:], " ")
			lines[0] = parts[0] + " " + wrapColor(status, colorStatus(code))
		}
	}

	for i := 1; i < len(lines); i++ {
		if lines[i] == "" {
			break
		}
		kv := strings.SplitN(lines[i], ":", 2)
		if len(kv) == 2 {
			lines[i] = wrapColor(strings.TrimSpace(kv[0]), colorHeader) + ":" + wrapColor(kv[1], colorString)
		}
	}
	return []byte(strings.Join(lines, "\r\n"))
}
