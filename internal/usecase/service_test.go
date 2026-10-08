package usecase

import (
	"context"
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/domain"
	"testing"
)

type tracedRepository struct {
	Repository
	calls int
}

func (r *tracedRepository) Test(context.Context) (string, error) { r.calls++; return "Hello!", nil }
func TestTestUseCaseCallsRepository(t *testing.T) {
	repo := &tracedRepository{}
	service := New(repo, nil, nil, 0, false)
	value, err := service.Test(context.Background())
	if err != nil || value != "Hello!" || repo.calls != 1 {
		t.Fatalf("test bypassed repository: %q, %v, %d", value, err, repo.calls)
	}
}

type rejectingRepository struct {
	Repository
	called bool
}

func (r *rejectingRepository) SaveEvent(context.Context, string, domain.Event, string, bool) (domain.Event, error) {
	r.called = true
	return domain.Event{}, nil
}
func TestInvalidEventNeverReachesStorage(t *testing.T) {
	repo := &rejectingRepository{}
	service := New(repo, nil, nil, 0, false)
	if _, err := service.Save(context.Background(), "owner", domain.Event{}, ""); err == nil || repo.called {
		t.Fatal("invalid event reached storage")
	}
}
