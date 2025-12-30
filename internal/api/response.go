package api

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
)

// writeJSON encodes v as JSON and writes it to the response.
// It buffers the encoding to detect errors before writing headers.
func writeJSON(w http.ResponseWriter, status int, v any) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(buf.Bytes()); err != nil {
		log.Printf("write response failed: %v", err)
	}
}
