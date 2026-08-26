package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func newTestHandler() *Handler {
	return NewHandler(NewService(NewMemoryRepository()))
}

// doRequest runs one request through the same route registration used
// in production.
func doRequest(t *testing.T, h *Handler, method, path string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(method, path, body)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestHealth(t *testing.T) {
	rec := doRequest(t, newTestHandler(), http.MethodGet, "/health", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"status":"ok"}` {
		t.Errorf("body = %q, want %q", got, `{"status":"ok"}`)
	}
}

func TestCreateAgent(t *testing.T) {
	rec := doRequest(t, newTestHandler(), http.MethodPost, "/api/v1/agents", strings.NewReader(`{
		"name": "paper-assistant",
		"description": "Help users read papers"
	}`))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var got Agent
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if got.ID == "" {
		t.Error("response ID is empty, want server-generated ID")
	}
	if _, err := uuid.Parse(got.ID); err != nil {
		t.Errorf("ID %q is not a parseable UUID: %v", got.ID, err)
	}
	if got.CreatedAt.IsZero() {
		t.Error("response CreatedAt is zero, want server-supplied timestamp")
	}
	if got.Name != "paper-assistant" {
		t.Errorf("name = %q, want %q", got.Name, "paper-assistant")
	}
	if got.Description != "Help users read papers" {
		t.Errorf("description = %q, want %q", got.Description, "Help users read papers")
	}
}

func TestCreateAgentTrimsName(t *testing.T) {
	rec := doRequest(t, newTestHandler(), http.MethodPost, "/api/v1/agents", strings.NewReader(`{"name":"  paper-assistant  "}`))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var got Agent
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if got.Name != "paper-assistant" {
		t.Errorf("name = %q, want trimmed %q", got.Name, "paper-assistant")
	}
	if got.CreatedAt.IsZero() {
		t.Error("response CreatedAt is zero, want server-supplied timestamp")
	}
}

func TestCreateAgentInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"missing name", `{"description":"x"}`},
		{"empty name", `{"name":"","description":"x"}`},
		{"whitespace name", `{"name":"   ","description":"x"}`},
		{"malformed JSON", `{"name": "oops`},
		{"trailing JSON", `{"name":"a"}{"name":"b"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doRequest(t, newTestHandler(), http.MethodPost, "/api/v1/agents", strings.NewReader(tt.body))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"error"`) {
				t.Errorf("body does not contain error object: %s", rec.Body.String())
			}
		})
	}
}

func TestListAgentsEmpty(t *testing.T) {
	rec := doRequest(t, newTestHandler(), http.MethodGet, "/api/v1/agents", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got []Agent
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("list = %#v, want empty non-nil slice (encoded as [])", got)
	}
}

func TestListAgents(t *testing.T) {
	h := newTestHandler()
	for _, name := range []string{"one", "two"} {
		rec := doRequest(t, h, http.MethodPost, "/api/v1/agents", strings.NewReader(`{"name":"`+name+`"}`))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %s: status = %d, want %d", name, rec.Code, http.StatusCreated)
		}
	}

	rec := doRequest(t, h, http.MethodGet, "/api/v1/agents", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got []Agent
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2; got %#v", len(got), got)
	}
	names := make(map[string]bool, len(got))
	for _, a := range got {
		if a.ID == "" {
			t.Error("agent in list has empty ID")
		}
		if a.CreatedAt.IsZero() {
			t.Error("agent in list has zero CreatedAt")
		}
		names[a.Name] = true
	}
	if !names["one"] || !names["two"] {
		t.Errorf("missing created agents; got names %v", names)
	}
}

func TestGetAgent(t *testing.T) {
	h := newTestHandler()
	createRec := doRequest(t, h, http.MethodPost, "/api/v1/agents", strings.NewReader(`{"name":"paper-assistant"}`))
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create: status = %d, want %d", createRec.Code, http.StatusCreated)
	}
	var created Agent
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("invalid create response: %v", err)
	}

	rec := doRequest(t, h, http.MethodGet, "/api/v1/agents/"+created.ID, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got Agent
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if got != created {
		t.Errorf("got %+v, want %+v", got, created)
	}
}

func TestGetAgentNotFound(t *testing.T) {
	rec := doRequest(t, newTestHandler(), http.MethodGet, "/api/v1/agents/nope", nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if !strings.Contains(rec.Body.String(), `"code":"agent_not_found"`) {
		t.Errorf("body = %s, want error code agent_not_found", rec.Body.String())
	}
}

// TestGetAgentValidUUIDNotFound covers a well-formed but absent UUID:
// it goes through the Repository, comes back as ErrAgentNotFound, and
// maps to 404 with the same error code as a malformed ID.
func TestGetAgentValidUUIDNotFound(t *testing.T) {
	rec := doRequest(t, newTestHandler(), http.MethodGet, "/api/v1/agents/"+uuid.NewString(), nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if !strings.Contains(rec.Body.String(), `"code":"agent_not_found"`) {
		t.Errorf("body = %s, want error code agent_not_found", rec.Body.String())
	}
}
