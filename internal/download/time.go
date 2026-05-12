package download

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

func firstTime(obj map[string]any, keys ...string) *time.Time {
	for _, key := range keys {
		v, ok := obj[key]
		if !ok {
			continue
		}
		switch t := v.(type) {
		case time.Time:
			tt := t.UTC()
			return &tt
		case json.Number:
			if parsed := unixTimeFromString(t.String()); parsed != nil {
				return parsed
			}
		case float64:
			if t > 0 {
				tt := time.Unix(int64(t), 0).UTC()
				return &tt
			}
		case string:
			if parsed := parseExternalTime(t); parsed != nil {
				return parsed
			}
		}
	}
	return nil
}

func parseExternalTime(raw string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if parsed := unixTimeFromString(raw); parsed != nil {
		return parsed
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"Mon Jan 02 15:04:05 -0700 2006",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			tt := t.UTC()
			return &tt
		}
	}
	return nil
}

func unixTimeFromString(raw string) *time.Time {
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || n <= 0 {
		return nil
	}
	if n > 100000000000 {
		n /= 1000
	}
	t := time.Unix(n, 0).UTC()
	return &t
}
