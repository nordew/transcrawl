package transcript

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var inlineTag = regexp.MustCompile(`<[^>]*>`)

var htmlEntities = strings.NewReplacer(
	"&amp;", "&",
	"&lt;", "<",
	"&gt;", ">",
	"&quot;", `"`,
	"&#39;", "'",
	"&nbsp;", " ",
)

func ToText(data []byte) (string, error) {
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "{") {
		return json3ToText([]byte(trimmed))
	}
	if strings.HasPrefix(trimmed, "WEBVTT") {
		return vttToText(trimmed), nil
	}
	if strings.Contains(trimmed, "-->") {
		return vttToText(trimmed), nil
	}
	return "", fmt.Errorf("unrecognized subtitle format")
}

type json3 struct {
	Events []struct {
		Segs []struct {
			UTF8 string `json:"utf8"`
		} `json:"segs"`
	} `json:"events"`
}

func json3ToText(data []byte) (string, error) {
	var doc json3
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", fmt.Errorf("parsing json3: %w", err)
	}
	lines := make([]string, 0, len(doc.Events))
	for _, ev := range doc.Events {
		if len(ev.Segs) == 0 {
			continue
		}
		var b strings.Builder
		for _, seg := range ev.Segs {
			b.WriteString(seg.UTF8)
		}
		lines = append(lines, cleanLine(b.String()))
	}
	return assemble(lines), nil
}

func vttToText(s string) string {
	var lines []string
	for _, raw := range strings.Split(s, "\n") {
		line := strings.TrimRight(raw, "\r")
		switch {
		case line == "",
			strings.HasPrefix(line, "WEBVTT"),
			strings.HasPrefix(line, "Kind:"),
			strings.HasPrefix(line, "Language:"),
			strings.HasPrefix(line, "NOTE"),
			strings.Contains(line, "-->"):
			continue
		}
		if isAllDigits(line) {
			continue
		}
		if cleaned := cleanLine(line); cleaned != "" {
			lines = append(lines, cleaned)
		}
	}
	return assemble(lines)
}

func cleanLine(s string) string {
	s = inlineTag.ReplaceAllString(s, "")
	s = htmlEntities.Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

func assemble(lines []string) string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		if len(out) > 0 {
			prev := out[len(out)-1]
			switch {
			case line == prev:
				continue
			case strings.HasPrefix(line, prev):
				out[len(out)-1] = line
				continue
			case strings.HasPrefix(prev, line):
				continue
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
