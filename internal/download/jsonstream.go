package download

import (
	"bytes"
	"encoding/json"
)

// JSONPayloads scans mixed CLI output and returns decoded JSON payloads. It
// tolerates object-per-line streams, pretty printed JSON, gallery-dl tuple
// arrays, and downloader log lines that begin with bracketed component names.
func JSONPayloads(output []byte) []any {
	var payloads []any
	for offset := 0; offset < len(output); {
		start := nextJSONLineStart(output, offset)
		if start < 0 {
			break
		}
		dec := json.NewDecoder(bytes.NewReader(output[start:]))
		dec.UseNumber()
		var payload any
		if err := dec.Decode(&payload); err != nil {
			offset = start + 1
			continue
		}
		payloads = append(payloads, payload)
		if n := dec.InputOffset(); n > 0 {
			offset = start + int(n)
		} else {
			offset = start + 1
		}
	}
	return payloads
}

func nextJSONLineStart(data []byte, from int) int {
	for i := from; i < len(data); {
		j := i
		for j < len(data) && (data[j] == ' ' || data[j] == '\t' || data[j] == '\r') {
			j++
		}
		if j < len(data) && (data[j] == '{' || data[j] == '[') && !looksLikeGalleryDLLogLine(data[j:]) {
			return j
		}
		if nl := bytes.IndexByte(data[i:], '\n'); nl >= 0 {
			i += nl + 1
		} else {
			break
		}
	}
	return -1
}

func looksLikeGalleryDLLogLine(line []byte) bool {
	if len(line) < 3 || line[0] != '[' {
		return false
	}
	c := line[1]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func FlattenJSONObjects(value any) []map[string]any {
	switch v := value.(type) {
	case map[string]any:
		return []map[string]any{v}
	case []any:
		var out []map[string]any
		for _, item := range v {
			out = append(out, FlattenJSONObjects(item)...)
		}
		return out
	default:
		return nil
	}
}
