package repository

import (
	"context"
	"errors"
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/domain"
	"os"
	"testing"
	"time"
)

func TestPostgresOwnershipOutboxAndRevision(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run against PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	r, err := OpenPostgres(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	id, err := domain.NewID()
	if err != nil {
		t.Fatal(err)
	}
	user := domain.User{ID: id, Name: "Repository integration test", Email: id + "@example.invalid", PasswordHash: "test-only-hash"}
	if err = r.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		r.pool.Exec(cleanup, "DELETE FROM event_outbox WHERE payload->>'userId'=$1", id)
		r.pool.Exec(cleanup, "DELETE FROM users WHERE id=$1", id)
	}()
	e, err := r.SaveEvent(ctx, id, event(), "", true)
	if err != nil {
		t.Fatal(err)
	}
	if e.ProcessingStatus != "pending" || e.Revision != 1 {
		t.Fatal("initial status or revision invalid")
	}
	events, err := r.ListEvents(ctx, "another-owner")
	if err != nil || len(events) != 0 {
		t.Fatal("owner isolation broken")
	}
	if _, err = r.SaveEvent(ctx, "another-owner", e, e.ID, true); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("cross-user update allowed")
	}
	if err = r.DeleteEvent(ctx, "another-owner", e.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("cross-user deletion allowed")
	}
	oldRevision := e.Revision
	e.Title = "Updated integration event"
	e, err = r.SaveEvent(ctx, id, e, e.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := r.ApplyStatus(ctx, domain.Job{EventID: e.ID, UserID: id, Revision: oldRevision, Status: "ready"})
	if err != nil || changed {
		t.Fatal("stale worker result changed a newer event")
	}
	changed, err = r.ApplyStatus(ctx, domain.Job{EventID: e.ID, UserID: id, Revision: e.Revision, Status: "ready"})
	if err != nil || !changed {
		t.Fatal("matching worker result not applied")
	}
	changed, err = r.ApplyStatus(ctx, domain.Job{EventID: e.ID, UserID: id, Revision: e.Revision, Status: "ready"})
	if err != nil || changed {
		t.Fatal("duplicate worker result not idempotent")
	}
	events, err = r.ListEvents(ctx, id)
	if err != nil || len(events) != 1 || events[0].ProcessingStatus != "ready" || events[0].Title != e.Title {
		t.Fatal("stored event invalid")
	}
	var count int
	if err = r.pool.QueryRow(ctx, "SELECT count(*) FROM event_outbox WHERE payload->>'userId'=$1", id).Scan(&count); err != nil || count != 2 {
		t.Fatal("create/update did not persist their outbox messages atomically")
	}
	jobs, err := r.ClaimJobs(ctx)
	if err != nil || len(jobs) != 2 {
		t.Fatalf("claim jobs: %d, %v", len(jobs), err)
	}
	leased, err := r.ClaimJobs(ctx)
	if err != nil || len(leased) != 0 {
		t.Fatal("active leases did not prevent concurrent claims")
	}
	if err = r.MarkPublished(ctx, jobs[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err = r.pool.Exec(ctx, "UPDATE event_outbox SET lease_until=now()-interval '1 second' WHERE id=$1", jobs[1].ID); err != nil {
		t.Fatal(err)
	}
	retried, err := r.ClaimJobs(ctx)
	if err != nil || len(retried) != 1 || retried[0].ID != jobs[1].ID {
		t.Fatal("expired lease lost a pending job")
	}
}
