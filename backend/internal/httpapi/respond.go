package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
)

// maxBodyBytes bounds JSON request bodies (1 MiB), far above any legitimate
// payload of this API.
const maxBodyBytes = 1 << 20

// problem is an RFC 9457 application/problem+json document. Code is a stable
// machine-readable identifier documented in the OpenAPI contract.
type problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

// writeJSON emits a JSON response.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// writeProblem emits an RFC 9457 error document.
func writeProblem(w http.ResponseWriter, status int, code, detail string) {
	title := http.StatusText(status)
	if title == "" {
		title = "Error"
	}
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem{
		Type:   "about:blank",
		Title:  title,
		Status: status,
		Code:   code,
		Detail: detail,
	})
}

// decodeJSON decodes a strict JSON body into dst. It rejects wrong content
// types, oversized bodies, unknown fields, and malformed JSON with a problem
// response and reports false. allowEmpty permits an entirely absent body.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any, allowEmpty bool) bool {
	if r.Body == nil {
		if allowEmpty {
			return true
		}
		writeProblem(w, http.StatusBadRequest, "invalid_json", "A JSON request body is required.")
		return false
	}
	if ct := r.Header.Get("Content-Type"); ct != "" {
		mediaType, _, err := mime.ParseMediaType(ct)
		if err != nil || !strings.EqualFold(mediaType, "application/json") {
			writeProblem(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json.")
			return false
		}
	}
	if r.ContentLength == 0 && allowEmpty {
		return true
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		switch {
		case errors.As(err, &maxErr):
			writeProblem(w, http.StatusRequestEntityTooLarge, "request_too_large", "The request body exceeds 1 MiB.")
			return false
		case err == io.EOF:
			if allowEmpty {
				return true
			}
			writeProblem(w, http.StatusBadRequest, "invalid_json", "A JSON request body is required.")
			return false
		default:
			writeProblem(w, http.StatusBadRequest, "validation_error", fmt.Sprintf("Invalid JSON body: %v", err))
			return false
		}
	}
	// Reject trailing content: a second JSON value or garbage after the
	// object must not be silently ignored.
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeProblem(w, http.StatusBadRequest, "invalid_json", "The request body must contain exactly one JSON object.")
		return false
	}
	return true
}

// readStrictBody bounds and validates the raw request body for handlers that
// hand r.Body to the WebAuthn library: it enforces the size limit, the JSON
// content type, and a single trailing-clean JSON value, then restores the
// body for downstream parsing.
func readStrictBody(w http.ResponseWriter, r *http.Request) bool {
	if ct := r.Header.Get("Content-Type"); ct != "" {
		mediaType, _, err := mime.ParseMediaType(ct)
		if err != nil || !strings.EqualFold(mediaType, "application/json") {
			writeProblem(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json.")
			return false
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeProblem(w, http.StatusRequestEntityTooLarge, "request_too_large", "The request body exceeds 1 MiB.")
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	var probe any
	if err := dec.Decode(&probe); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_json", "The request body must be a JSON object.")
		return false
	}
	if err := dec.Decode(&probe); err != io.EOF {
		writeProblem(w, http.StatusBadRequest, "invalid_json", "The request body must contain exactly one JSON object.")
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return true
}
