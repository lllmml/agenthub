package httpx

import (
	"encoding/json"
	"net/http"
)

// WriteJSON writes v as a JSON response with the given status code.
// Content-Type must be set before WriteHeader, so it is set first.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError writes a JSON error response in the shape:
//
//	{"error": {"code": "...", "message": "..."}}
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}
