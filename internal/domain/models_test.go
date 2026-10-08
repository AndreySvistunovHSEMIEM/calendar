package domain

import (
	"strings"
	"testing"
)

func testEvent() Event {
	return Event{Title: "Встреча", Date: "2026-10-08", Start: "10:00", End: "11:00", Category: "work"}
}
func validateEvent(e *Event) error { return ValidateEvent(e) }
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
