package agent

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/lllmml/agenthub/internal/httpx"
)

// Handler is the HTTP layer for agents. It parses requests, calls the
// service, and maps service errors to HTTP status codes. It contains no
// storage or business logic.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes mounts the agent HTTP routes on mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("GET /api/v1/agents", h.list)
	mux.HandleFunc("POST /api/v1/agents", h.create)
	mux.HandleFunc("GET /api/v1/agents/{id}", h.get)
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in CreateAgentInput
	if err := decodeJSON(r, &in); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", "request body must be a single JSON object")
		return
	}

	created, err := h.svc.Create(r.Context(), in)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidAgentName):
			httpx.WriteError(w, http.StatusBadRequest, "invalid_agent_name", err.Error())
		default:
			// Do not leak internal error details to clients.
			httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		}
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, created)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	agents, err := h.svc.List(r.Context())
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, agents)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	a, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, ErrAgentNotFound):
			httpx.WriteError(w, http.StatusNotFound, "agent_not_found", err.Error())
		default:
			httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		}
		return
	}

	httpx.WriteJSON(w, http.StatusOK, a)
}

// decodeJSON decodes exactly one JSON value from the request body and
// rejects malformed JSON as well as any trailing content after it.
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		return err
	}
	// A second successful decode (or a syntax error) means the body
	// contains trailing JSON content; only io.EOF is acceptable.
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return errors.New("trailing JSON content")
	}
	return nil
}
