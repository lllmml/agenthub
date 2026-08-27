package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestServiceCreateBlankName(t *testing.T) {
	svc := NewService(NewMemoryRepository(), NewNoopAgentCache())
	for _, name := range []string{"", "   ", "\t\n"} {
		_, err := svc.Create(context.Background(), CreateAgentInput{Name: name})
		if !errors.Is(err, ErrInvalidAgentName) {
			t.Errorf("Create(%q) err = %v, want ErrInvalidAgentName", name, err)
		}
	}
}

func TestServiceCreateGeneratesUUIDAndTimestamp(t *testing.T) {
	svc := NewService(NewMemoryRepository(), NewNoopAgentCache())
	created, err := svc.Create(context.Background(), CreateAgentInput{Name: "paper-assistant", Description: "reads papers"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := uuid.Parse(created.ID); err != nil {
		t.Errorf("ID %q is not a parseable UUID: %v", created.ID, err)
	}
	if created.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero, want a meaningful timestamp")
	}
}

func TestServiceGetByIDNotFound(t *testing.T) {
	svc := NewService(NewMemoryRepository(), NewNoopAgentCache())
	_, err := svc.GetByID(context.Background(), uuid.NewString())
	if !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("err = %v, want ErrAgentNotFound", err)
	}
}

// spyRepository is a minimal Repository fake that records GetByID
// calls. It lets tests assert the Service does not consult storage for
// malformed IDs without a mock framework.
type spyRepository struct {
	agents   map[string]Agent
	getCalls []string
}

var _ Repository = (*spyRepository)(nil)

func newSpyRepository() *spyRepository {
	return &spyRepository{agents: make(map[string]Agent)}
}

func (s *spyRepository) Create(ctx context.Context, a Agent) (Agent, error) {
	s.agents[a.ID] = a
	return a, nil
}

func (s *spyRepository) GetByID(ctx context.Context, id string) (Agent, error) {
	s.getCalls = append(s.getCalls, id)
	a, ok := s.agents[id]
	if !ok {
		return Agent{}, ErrAgentNotFound
	}
	return a, nil
}

func (s *spyRepository) List(ctx context.Context) ([]Agent, error) {
	out := make([]Agent, 0, len(s.agents))
	for _, a := range s.agents {
		out = append(out, a)
	}
	return out, nil
}

func TestServiceGetByIDInvalidUUID(t *testing.T) {
	spy := newSpyRepository()
	svc := NewService(spy, NewNoopAgentCache())

	for _, id := range []string{"nope", "", "not-a-uuid", "12345", "zzzzzzzz-zzzz-zzzz-zzzz-zzzzzzzzzzzz"} {
		if _, err := svc.GetByID(context.Background(), id); !errors.Is(err, ErrAgentNotFound) {
			t.Errorf("GetByID(%q) err = %v, want ErrAgentNotFound", id, err)
		}
	}
	if len(spy.getCalls) != 0 {
		t.Errorf("repository was consulted for invalid UUIDs: %v", spy.getCalls)
	}
}

func TestServiceGetByIDValidUUIDReachesRepository(t *testing.T) {
	spy := newSpyRepository()
	svc := NewService(spy, NewNoopAgentCache())

	id := uuid.NewString()
	if _, err := svc.GetByID(context.Background(), id); !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("err = %v, want ErrAgentNotFound", err)
	}
	if len(spy.getCalls) != 1 || spy.getCalls[0] != id {
		t.Errorf("repository GetByID calls = %v, want [%s]", spy.getCalls, id)
	}
}

// TestServiceGetByIDNormalizesAlternateUUIDForms verifies the Service
// hands the canonical 36-character lowercase UUID to the repository
// even when the request used a form uuid.Parse accepts but PostgreSQL
// does not accept as a raw string (urn:uuid:, braced, hyphen-free).
func TestServiceGetByIDNormalizesAlternateUUIDForms(t *testing.T) {
	id := uuid.NewString()

	forms := map[string]string{
		"urn":        "urn:uuid:" + id,
		"braced":     "{" + id + "}",
		"no-hyphens": strings.ReplaceAll(id, "-", ""),
	}
	for name, form := range forms {
		t.Run(name, func(t *testing.T) {
			spy := newSpyRepository()
			svc := NewService(spy, NewNoopAgentCache())

			if _, err := svc.GetByID(context.Background(), form); !errors.Is(err, ErrAgentNotFound) {
				t.Fatalf("GetByID(%q) err = %v, want ErrAgentNotFound", form, err)
			}
			if len(spy.getCalls) != 1 || spy.getCalls[0] != id {
				t.Errorf("repository GetByID calls = %v, want [%s]", spy.getCalls, id)
			}
		})
	}
}
