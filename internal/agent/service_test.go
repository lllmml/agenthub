package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestServiceCreateBlankName(t *testing.T) {
	svc := NewService(NewMemoryRepository())
	for _, name := range []string{"", "   ", "\t\n"} {
		_, err := svc.Create(context.Background(), CreateAgentInput{Name: name})
		if !errors.Is(err, ErrInvalidAgentName) {
			t.Errorf("Create(%q) err = %v, want ErrInvalidAgentName", name, err)
		}
	}
}

func TestServiceCreateGeneratesUUIDAndTimestamp(t *testing.T) {
	svc := NewService(NewMemoryRepository())
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
	svc := NewService(NewMemoryRepository())
	_, err := svc.GetByID(context.Background(), uuid.NewString())
	if !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("err = %v, want ErrAgentNotFound", err)
	}
}
