package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io"
	"sort"
	"strconv"
	"strings"
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
	colorResMarker = "\033[36m"
)

func wrapColor(s, color string) string {
	return color + s + colorReset
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
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return string(data)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			b.WriteString(strings.Repeat("  ", indent))
			b.WriteString(wrapColor("<"+t.Name.Local, colorTag))
			for _, attr := range t.Attr {
				b.WriteString(" ")
				b.WriteString(wrapColor(attr.Name.Local, colorAttr))
				b.WriteString(wrapColor("=", colorPunct))
				b.WriteString(wrapColor("\""+attr.Value+"\"", colorString))
			}
			b.WriteString(wrapColor(">", colorTag))
			b.WriteString("\n")
			indent++
		case xml.EndElement:
			indent--
			b.WriteString(strings.Repeat("  ", indent))
			b.WriteString(wrapColor("</"+t.Name.Local+">", colorTag))
			b.WriteString("\n")
		case xml.CharData:
			txt := strings.TrimSpace(string([]byte(t)))
			if len(txt) > 0 {
				b.WriteString(strings.Repeat("  ", indent))
				b.WriteString(wrapColor(txt, colorString))
				b.WriteString("\n")
			}
		case xml.Comment:
			b.WriteString(strings.Repeat("  ", indent))
			b.WriteString(wrapColor("<!--"+string(t)+"-->", colorNull))
			b.WriteString("\n")
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
