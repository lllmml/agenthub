package agent

import "errors"

// Agent is the domain model for an AI agent.
type Agent struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// CreateAgentInput is the payload for creating an agent.
type CreateAgentInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Sentinel errors returned by the agent domain. Handlers map these to
// HTTP status codes via errors.Is, so callers must never compare strings.
var (
	ErrAgentNotFound    = errors.New("agent not found")
	ErrInvalidAgentName = errors.New("agent name is required")
)
