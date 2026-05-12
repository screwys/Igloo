package download

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

func ClassifyError(err error, output []byte) string {
	text := strings.ToLower(string(output))
	if err != nil {
		text += "\n" + strings.ToLower(err.Error())
	}
	switch {
	case errors.Is(err, context.Canceled):
		return ErrorKindCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return ErrorKindTemporary
	}
	var httpErr *HTTPStatusError
	if errors.As(err, &httpErr) {
		if httpErr.StatusCode == 429 {
			return ErrorKindRateLimit
		}
		if httpErr.StatusCode == 404 || httpErr.StatusCode == 410 {
			return ErrorKindNotFound
		}
		if httpErr.StatusCode >= 400 && httpErr.StatusCode < 500 {
			if containsAny(text, "login", "auth", "cookie", "unauthorized") {
				return ErrorKindAuth
			}
			return ErrorKindPermanentHTTP
		}
		if httpErr.StatusCode >= 500 {
			return ErrorKindTemporary
		}
	}
	if containsAny(text, "429", "too many requests", "rate limit", "ratelimit") {
		return ErrorKindRateLimit
	}
	if containsAny(text, "login required", "not logged in", "authentication", "unauthorized", "cookie", "cookies", "forbidden") {
		return ErrorKindAuth
	}
	if containsAny(text, "not found", "notfound", "404", "410", "no such user", "requested post not available", "unavailable") {
		return ErrorKindNotFound
	}
	if containsAny(text, "no files downloaded", "no results", "empty result", "returned no info", "returned no") {
		return ErrorKindEmptyResult
	}
	if containsAny(text, "invalid character", "unexpected eof", "parse", "decode", "unmarshal") {
		return ErrorKindParse
	}
	if containsAny(text, "timeout", "temporary", "temporarily", "connection reset", "connection refused", "tls handshake", "i/o timeout", "deadline exceeded") {
		return ErrorKindTemporary
	}
	if err != nil {
		return ErrorKindUnknown
	}
	return ""
}

func ErrorKind(err error) string {
	return ClassifyError(err, nil)
}

func commandError(tool string, result CommandResult) error {
	if result.Err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w: %s", tool, result.Err, RedactText(string(result.CombinedOutput())))
}

func errorString(err error, output []byte) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if len(output) > 0 {
		msg += ": " + string(output)
	}
	return RedactText(msg)
}

func containsAny(s string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}
