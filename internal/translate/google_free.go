package translate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/screwys/igloo/internal/language"
	"github.com/screwys/igloo/internal/settings"
)

const googleFreeEndpoint = "https://translate.google.com/_/TranslateWebserverUi/data/batchexecute"

type googleFreeSegment struct {
	request int
	rpc     int
	text    string
}

func googleFreeTranslateBatch(ctx context.Context, requests []translationTextRequest, targetLang string) ([]*Result, error) {
	if len(requests) == 0 {
		return nil, nil
	}
	target := strings.ToLower(strings.TrimSpace(targetLang))
	if target == "nb" {
		target = "no"
	}
	var segments []googleFreeSegment
	var calls [][]any
	callIDs := make(map[string]int)
	for i, request := range requests {
		for _, text := range splitGoogleFreeText(request.Text) {
			id, exists := callIDs[text]
			if !exists {
				payload, err := json.Marshal([]any{[]any{text, "auto", target, 1}, []any{}})
				if err != nil {
					return nil, err
				}
				id = len(calls)
				calls = append(calls, []any{"MkEWBc", string(payload), nil, strconv.Itoa(id)})
				callIDs[text] = id
			}
			segments = append(segments, googleFreeSegment{request: i, rpc: id, text: text})
		}
	}
	payload, err := json.Marshal([]any{calls})
	if err != nil {
		return nil, err
	}
	body := url.Values{"f.req": {string(payload)}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, googleFreeEndpoint, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, ProviderRateLimitedError{Provider: settings.TranslateBackendGoogleFree}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("google free translate status %d", resp.StatusCode)
	}
	response, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	translated, err := parseGoogleFreeBatch(response, len(calls))
	if err != nil {
		return nil, err
	}

	results := make([]*Result, len(requests))
	for i := range requests {
		var text strings.Builder
		var sourceLang, separator string
		complete := true
		for _, segment := range segments {
			if segment.request != i {
				continue
			}
			result := translated[segment.rpc]
			if result == nil {
				complete = false
				break
			}
			text.WriteString(separator)
			text.WriteString(result.TranslatedText)
			if sourceLang == "" {
				sourceLang = result.SourceLang
			}
			separator = " "
			if strings.HasSuffix(segment.text, "\n") {
				separator = "\n"
			}
		}
		if !complete || text.Len() == 0 {
			continue
		}
		if sourceLang == "" {
			sourceLang = requests[i].SourceLangHint
		}
		results[i] = &Result{
			TranslatedText: text.String(),
			SourceLang:     language.DisplayName(sourceLang),
			TargetLang:     targetLang,
		}
	}
	return results, nil
}

// Google rejects oversized texts inside an otherwise successful batch. Split
// at 4,000 UTF-16 units, keeping word boundaries and protected tokens intact.
func splitGoogleFreeText(text string) []string {
	var chunks []string
	for text != "" {
		end, boundary, units := len(text), 0, 0
		for i, r := range text {
			units++
			if r > 0xffff {
				units++
			}
			if units > 4000 {
				end = i
				if boundary > 0 {
					end = boundary
				}
				break
			}
			if unicode.IsSpace(r) || strings.ContainsRune(".!?。！？", r) {
				boundary = i + utf8.RuneLen(r)
			}
		}
		if end < len(text) {
			if start := strings.LastIndex(text[:end], "{{"); start > 0 {
				if token := reSentinel.FindStringIndex(text[start:]); token != nil && token[0] == 0 && start+token[1] > end {
					end = start
				}
			}
		}
		chunks = append(chunks, text[:end])
		text = text[end:]
	}
	return chunks
}

func parseGoogleFreeBatch(response []byte, count int) ([]*Result, error) {
	results := make([]*Result, count)
	seen := make([]bool, count)
	found := false
	// The response has an XSSI prefix, byte counts, and JSON frames. Each RPC
	// reply carries its request ID, and replies can arrive in any order.
	for _, line := range bytes.Split(response, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] != '[' {
			continue
		}
		var records [][]json.RawMessage
		if err := json.Unmarshal(line, &records); err != nil {
			return nil, fmt.Errorf("%w: invalid google batch frame", ErrTranslationFailed)
		}
		for _, record := range records {
			var kind string
			if len(record) == 0 || json.Unmarshal(record[0], &kind) != nil {
				return nil, ErrTranslationFailed
			}
			if kind != "wrb.fr" {
				continue
			}
			var rpc, id string
			if len(record) < 7 || json.Unmarshal(record[1], &rpc) != nil || rpc != "MkEWBc" || json.Unmarshal(record[6], &id) != nil {
				return nil, ErrTranslationFailed
			}
			i, err := strconv.Atoi(id)
			if err != nil || i < 0 || i >= count || seen[i] {
				return nil, fmt.Errorf("%w: invalid google batch request ID", ErrTranslationFailed)
			}
			seen[i], found = true, true
			result, err := parseGoogleFreeTranslation(record[2])
			if err == nil {
				results[i] = result
			}
		}
	}
	if !found {
		return nil, ErrTranslationFailed
	}
	return results, nil
}

func parseGoogleFreeTranslation(raw json.RawMessage) (*Result, error) {
	var payload string
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	sentences, err := googleFreeArray(json.RawMessage(payload), 1, 0, 0, 5)
	if err != nil {
		return nil, err
	}
	var parts []string
	for _, sentence := range sentences {
		fields, err := googleFreeArray(sentence)
		if err != nil || len(fields) == 0 {
			return nil, ErrTranslationFailed
		}
		var text string
		if err := json.Unmarshal(fields[0], &text); err != nil {
			return nil, err
		}
		parts = append(parts, text)
	}
	text := strings.TrimSpace(strings.Join(parts, " "))
	if text == "" {
		return nil, ErrTranslationFailed
	}
	source, err := googleFreeArray(json.RawMessage(payload), 0)
	if err != nil || len(source) < 3 {
		return nil, ErrTranslationFailed
	}
	var sourceLang string
	if err := json.Unmarshal(source[2], &sourceLang); err != nil {
		return nil, err
	}
	return &Result{TranslatedText: text, SourceLang: sourceLang}, nil
}

func googleFreeArray(raw json.RawMessage, path ...int) ([]json.RawMessage, error) {
	for {
		var values []json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil {
			return nil, err
		}
		if len(path) == 0 {
			return values, nil
		}
		if path[0] >= len(values) {
			return nil, ErrTranslationFailed
		}
		raw, path = values[path[0]], path[1:]
	}
}
