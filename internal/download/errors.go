package download

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type FailureClassification struct {
	Kind       string
	Permanent  bool
	RetryDelay time.Duration
}

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

func ClassifyFailure(err error, output []byte, attempt int) FailureClassification {
	kind := ClassifyError(err, output)
	permanent := false
	switch kind {
	case ErrorKindAuth, ErrorKindPermanentHTTP, ErrorKindEmptyResult:
		permanent = true
	}
	if err == nil || permanent {
		return FailureClassification{Kind: kind, Permanent: permanent}
	}
	delay := retryDelayForKind(kind, attempt)
	return FailureClassification{Kind: kind, RetryDelay: delay}
}

func retryDelayForKind(kind string, attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	switch kind {
	case ErrorKindRateLimit:
		delay := time.Hour
		for i := 1; i < attempt && delay < 12*time.Hour; i++ {
			delay *= 2
		}
		if delay > 12*time.Hour {
			return 12 * time.Hour
		}
		return delay
	default:
		delay := 30 * time.Second
		for i := 0; i < attempt && delay < 6*time.Hour; i++ {
			delay *= 2
		}
		if delay > 6*time.Hour {
			return 6 * time.Hour
		}
		return delay
	}
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
