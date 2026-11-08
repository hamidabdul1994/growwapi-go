package growwapi

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

// parseYAMLMap parses a tiny subset of YAML sufficient for our config file:
// top-level keys mapped to scalar values (string / int / bool). Quoted
// strings are preserved; lists are not supported at the top level because
// our file config is flat.
func parseYAMLMap(data []byte) (map[string]string, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	out := map[string]string{}
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimRight(scanner.Text(), " \t\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "-") {
			return nil, fmt.Errorf("line %d: top-level lists are not supported", lineNo)
		}
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			return nil, fmt.Errorf("line %d: expected key:value", lineNo)
		}
		key := strings.TrimSpace(line[:colon])
		raw := strings.TrimSpace(line[colon+1:])
		raw = strings.TrimSuffix(raw, "#")
		raw = strings.TrimSpace(raw)

		if strings.HasPrefix(raw, "\"") && strings.HasSuffix(raw, "\"") && len(raw) >= 2 {
			raw = raw[1 : len(raw)-1]
		} else if strings.HasPrefix(raw, "'") && strings.HasSuffix(raw, "'") && len(raw) >= 2 {
			raw = raw[1 : len(raw)-1]
		}

		if raw == "" || raw == "|" || raw == ">" {
			continue
		}

		if _, err := strconv.ParseInt(raw, 10, 64); err == nil {
			out[key] = raw
			continue
		}
		if b, err := strconv.ParseBool(raw); err == nil {
			out[key] = strconv.FormatBool(b)
			continue
		}
		out[key] = raw
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	return out, nil
}
