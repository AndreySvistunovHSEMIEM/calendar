package transport

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func testEvent() Event {
	return Event{Title: "Встреча", Date: "2026-10-08", Start: "10:00", End: "11:00", Category: "work"}
}
func TestICSCalendarEscapingAndDates(t *testing.T) {
	e := testEvent()
	e.ID = "test"
	e.Title = strings.Repeat("Космическая встреча, ", 10)
	e.Description = "Первая строка\r\nВторая; строка\\текст"
	allDay := e
	allDay.ID, allDay.Date, allDay.AllDay = "all-day", "2024-02-29", true
	ics := calendarICS([]Event{e, allDay}, time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC))
	if !utf8.ValidString(ics) {
		t.Fatal("UTF-8 split during folding")
	}
	for _, line := range strings.Split(ics, "\r\n") {
		if len(line) > 75 {
			t.Errorf("line has %d octets", len(line))
		}
	}
	unfolded := strings.ReplaceAll(ics, "\r\n ", "")
	for _, required := range []string{"DTSTART:20261008T100000", "DTEND:20261008T110000", "DTSTART;VALUE=DATE:20240229", "DTEND;VALUE=DATE:20240301", "DTSTAMP:20261008T120000Z", `DESCRIPTION:Первая строка\nВторая\; строка\\текст`, `встреча\,`} {
		if !strings.Contains(unfolded, required) {
			t.Errorf("missing ICS field: %s", required)
		}
	}
}
