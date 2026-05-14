package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

const requestBodyTooLargeMessage = "request body too large"

const defaultJSONMaxBodyBytes int64 = 1 << 20

var errRequestBodyTooLarge = errors.New(requestBodyTooLargeMessage)

func requestContentLengthTooLarge(r *http.Request, maxBytes int64) bool {
	return maxBytes >= 0 && r.ContentLength > maxBytes
}

func requestBodyTooLarge(err error) bool {
	if errors.Is(err, errRequestBodyTooLarge) {
		return true
	}
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}

func limitRequestBody(w http.ResponseWriter, r *http.Request, maxBytes int64) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
}

func decodeLimitedJSON(w http.ResponseWriter, r *http.Request, maxBytes int64, into any) error {
	if requestContentLengthTooLarge(r, maxBytes) {
		return errRequestBodyTooLarge
	}
	limitRequestBody(w, r, maxBytes)
	if err := json.NewDecoder(r.Body).Decode(into); err != nil {
		if requestBodyTooLarge(err) {
			return errRequestBodyTooLarge
		}
		return err
	}
	return nil
}

func decodeJSON(w http.ResponseWriter, r *http.Request, into any) error {
	return decodeLimitedJSON(w, r, defaultJSONMaxBodyBytes, into)
}

func readLimitedBody(r io.Reader, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, errRequestBodyTooLarge
	}
	return data, nil
}
