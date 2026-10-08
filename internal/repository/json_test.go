package repository

import (
	"context"
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/domain"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func fixture(t *testing.T) *JSON {
	t.Helper()
	r, err := OpenJSON(filepath.Join(t.TempDir(), "calendar.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = r.CreateUser(context.Background(), domain.User{ID: "owner", Name: "Owner", Email: "owner@example.invalid", PasswordHash: "test-only-hash"}); err != nil {
		t.Fatal(err)
	}
	return r
}
func event() domain.Event {
	return domain.Event{Title: "Встреча", Date: "2026-10-08", Start: "10:00", End: "11:00", Category: "work"}
}
func TestFileLifecycleAndReload(t *testing.T) {
	r := fixture(t)
	ctx := context.Background()
	e, err := r.SaveEvent(ctx, "owner", event(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	e.Title = "Изменено"
	e.Completed = true
	e, err = r.SaveEvent(ctx, "owner", e, e.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	session := domain.Session{ID: "session", UserID: "owner", ExpiresAt: time.Now().Add(time.Hour)}
	if err = r.SaveSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	loaded, err := OpenJSON(r.path)
	if err != nil {
		t.Fatal(err)
	}
	items, err := loaded.ListEvents(ctx, "owner")
	if err != nil || len(items) != 1 || items[0] != e {
		t.Fatalf("reload: %#v %v", items, err)
	}
	if _, err = loaded.Session(ctx, "session"); err != nil {
		t.Fatal(err)
	}
	if err = loaded.DeleteEvent(ctx, "other", e.ID); err != domain.ErrNotFound {
		t.Fatal("owner boundary broken")
	}
	if err = loaded.DeleteEvent(ctx, "owner", e.ID); err != nil {
		t.Fatal(err)
	}
	loaded, err = OpenJSON(r.path)
	if err != nil {
		t.Fatal(err)
	}
	items, _ = loaded.ListEvents(ctx, "owner")
	if len(items) != 0 {
		t.Fatal("delete not persisted")
	}
}
func TestConcurrentWritesPersistEveryEvent(t *testing.T) {
	r := fixture(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := r.SaveEvent(context.Background(), "owner", event(), "", false); err != nil {
				t.Error(err)
			}
			r.ListEvents(context.Background(), "owner")
		}()
	}
	wg.Wait()
	loaded, err := OpenJSON(r.path)
	if err != nil {
		t.Fatal(err)
	}
	items, _ := loaded.ListEvents(context.Background(), "owner")
	if len(items) != 20 {
		t.Fatalf("lost concurrent writes: %d", len(items))
	}
}
func TestFailedWriteLeavesMemoryUnchanged(t *testing.T) {
	r := fixture(t)
	ctx := context.Background()
	e, err := r.SaveEvent(ctx, "owner", event(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(filepath.Dir(r.path), "blocked")
	if err = os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	r.path = blocked
	changed := e
	changed.Title = "Not saved"
	if _, err = r.SaveEvent(ctx, "owner", changed, e.ID, false); err == nil {
		t.Fatal("expected failed update")
	}
	if err = r.DeleteEvent(ctx, "owner", e.ID); err == nil {
		t.Fatal("expected failed delete")
	}
	items, _ := r.ListEvents(ctx, "owner")
	if len(items) != 1 || items[0] != e {
		t.Fatal("failed write changed memory")
	}
}
func TestCorruptFileNeverOverwritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calendar.json")
	broken := []byte("{broken")
	if err := os.WriteFile(path, broken, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenJSON(path); err == nil {
		t.Fatal("corrupt file accepted")
	}
	data, _ := os.ReadFile(path)
	if string(data) != string(broken) {
		t.Fatal("corrupt file overwritten")
	}
}
