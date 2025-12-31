package api

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
)

// errorResponse is the standard error response format.
type errorResponse struct {
	Error string `json:"error"`
}

// writeJSON encodes v as JSON and writes it to the response.
// It buffers the encoding to detect errors before writing headers.
func writeJSON(w http.ResponseWriter, status int, v any) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		log.Printf("json encode failed: %v", err)
		writeErrorFallback(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(buf.Bytes()); err != nil {
		log.Printf("write response failed: %v", err)
	}
}

// writeError writes a JSON error response with consistent format.
// For 5xx errors, the underlying error is logged for debugging.
// The public message is what clients see; use generic messages for 5xx.
func writeError(w http.ResponseWriter, status int, public string, err error) {
	if public == "" {
		public = http.StatusText(status)
	}
	if status >= 500 && err != nil {
		log.Printf("internal error: %v", err)
	}
	writeJSON(w, status, errorResponse{Error: public})
}

// writeErrorFallback writes a plain text error when JSON encoding fails.
// This is a last-resort fallback to avoid infinite recursion.
func writeErrorFallback(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	w.Write([]byte(message))
}
