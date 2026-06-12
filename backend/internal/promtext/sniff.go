package promtext

import (
	"regexp"
	"strings"
)

var sampleLinePattern = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*(?:\{[^}\r\n]*\})?\s+[-+]?(?:(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][-+]?\d+)?|Inf|NaN)(?:\s+\d+)?$`)

// Sniff reports whether body looks like Prometheus text exposition format.
// It is the cheap, lenient counterpart to Parse, used when probing candidate
// endpoints: a HELP/TYPE comment or one well-formed sample line is enough,
// and malformed lines elsewhere in the body do not disqualify it.
func Sniff(body []byte) bool {
	text := strings.TrimSpace(string(body))
	if text == "" {
		return false
	}

	lower := strings.ToLower(text)
	if strings.HasPrefix(lower, "<!doctype html") ||
		strings.HasPrefix(lower, "<html") ||
		strings.HasPrefix(lower, "{") ||
		strings.HasPrefix(lower, "[") {
		return false
	}

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "# HELP ") || strings.HasPrefix(line, "# TYPE ") {
			return true
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if sampleLinePattern.MatchString(line) {
			return true
		}
	}

	return false
}
