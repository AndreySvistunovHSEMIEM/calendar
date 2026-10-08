package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testEvent() Event {
	return Event{Title: "Тестовая встреча", Date: "2026-10-08", Start: "10:00", End: "11:00", Category: "work"}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := openStore(filepath.Join(t.TempDir(), "events.json"), false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStoreLifecycleAndReload(t *testing.T) {
	s := newTestStore(t)
	e, err := s.save(testEvent(), "")
	if err != nil || e.ID == "" {
		t.Fatalf("create: %#v, %v", e, err)
	}
	e.Title, e.Completed = "Изменённое событие", true
	if _, err := s.save(e, e.ID); err != nil {
		t.Fatal(err)
	}
	reloaded, err := openStore(s.path, true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if events := reloaded.list(); len(events) != 1 || events[0] != e {
		t.Fatalf("lost event on reload: %#v", events)
	}
	if err := reloaded.delete(e.ID); err != nil {
		t.Fatal(err)
	}
	again, err := openStore(s.path, true, time.Now())
	if err != nil || len(again.list()) != 0 {
		t.Fatalf("delete not persisted, or demo reseeded: %v", err)
	}
	if _, err := again.save(e, "missing"); err != errNotFound {
		t.Fatalf("missing update: %v", err)
	}
}

func TestValidationCalendarBoundaries(t *testing.T) {
	tests := []struct {
		name  string
		edit  func(*Event)
		valid bool
	}{
		{"leap day", func(e *Event) { e.Date = "2024-02-29" }, true},
		{"not leap day", func(e *Event) { e.Date = "2025-02-29" }, false},
		{"invalid month", func(e *Event) { e.Date = "2026-13-01" }, false},
		{"missing leading zero", func(e *Event) { e.Date = "2026-1-08" }, false},
		{"out of range", func(e *Event) { e.Date = "2101-01-01" }, false},
		{"equal times", func(e *Event) { e.End = e.Start }, false},
		{"overnight", func(e *Event) { e.Start, e.End = "23:00", "01:00" }, false},
		{"invalid hour", func(e *Event) { e.End = "24:00" }, false},
		{"invalid category", func(e *Event) { e.Category = "unknown" }, false},
		{"blank title", func(e *Event) { e.Title = "  " }, false},
		{"unicode title", func(e *Event) { e.Title = strings.Repeat("Я", 100) }, true},
		{"long unicode title", func(e *Event) { e.Title = strings.Repeat("Я", 101) }, false},
		{"all day clears times", func(e *Event) { e.AllDay = true; e.Start, e.End = "bad", "bad" }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := testEvent()
			tt.edit(&e)
			err := validateEvent(&e)
			if (err == nil) != tt.valid {
				t.Fatalf("validation: %v", err)
			}
			if e.AllDay && (e.Start != "" || e.End != "") {
				t.Fatal("all-day times were not cleared")
			}
		})
	}
}

func TestConcurrentWritesPersistEveryEvent(t *testing.T) {
	s := newTestStore(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.save(testEvent(), ""); err != nil {
				t.Error(err)
			}
			s.list()
		}()
	}
	wg.Wait()
	reloaded, err := openStore(s.path, false, time.Now())
	if err != nil || len(reloaded.list()) != 20 {
		t.Fatalf("concurrent events lost: %v", err)
	}
}

func TestFailedWriteLeavesMemoryUnchanged(t *testing.T) {
	s := newTestStore(t)
	e, err := s.save(testEvent(), "")
	if err != nil {
		t.Fatal(err)
	}
	// A directory cannot be replaced by the temporary JSON file.
	blocked := filepath.Join(filepath.Dir(s.path), "blocked")
	if err := os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	s.path = blocked
	changed := e
	changed.Title = "Не должно сохраниться"
	if _, err := s.save(changed, e.ID); err == nil {
		t.Fatal("expected failed update")
	}
	if err := s.delete(e.ID); err == nil {
		t.Fatal("expected failed delete")
	}
	if events := s.list(); len(events) != 1 || events[0] != e {
		t.Fatalf("failed write changed memory: %#v", events)
	}
}

func TestCorruptStorageNeverOverwritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.json")
	broken := []byte("{broken data")
	if err := os.WriteFile(path, broken, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := openStore(path, true, time.Now()); err == nil {
		t.Fatal("corrupt data silently accepted")
	}
	data, _ := os.ReadFile(path)
	if string(data) != string(broken) {
		t.Fatal("corrupt data was overwritten")
	}
	duplicate := eventFile{Version: 1, Events: []Event{testEvent(), testEvent()}}
	duplicate.Events[0].ID, duplicate.Events[1].ID = "same", "same"
	data, _ = json.Marshal(duplicate)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := openStore(path, false, time.Now()); err == nil {
		t.Fatal("duplicate IDs accepted")
	}
}
