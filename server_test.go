package main

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func testHandler(t *testing.T) http.Handler {
	t.Helper()
	web, err := fs.Sub(frontend, "web")
	if err != nil {
		t.Fatal(err)
	}
	return newHandler(newTestStore(t), web)
}

func apiCall(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestAPICompleteLifecycle(t *testing.T) {
	h := testHandler(t)
	body, _ := json.Marshal(testEvent())
	w := apiCall(h, "POST", "/api/events", string(body))
	if w.Code != 201 {
		t.Fatalf("POST: %d %s", w.Code, w.Body)
	}
	var e Event
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	e.Title = "Обновлено"
	body, _ = json.Marshal(e)
	if w = apiCall(h, "PUT", "/api/events/"+e.ID, string(body)); w.Code != 200 {
		t.Fatalf("PUT: %d %s", w.Code, w.Body)
	}
	if w = apiCall(h, "GET", "/api/events", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "Обновлено") {
		t.Fatalf("GET: %d %s", w.Code, w.Body)
	}
	if w = apiCall(h, "GET", "/api/export", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "SUMMARY:Обновлено") {
		t.Fatalf("export: %d %s", w.Code, w.Body)
	}
	if w = apiCall(h, "DELETE", "/api/events/"+e.ID, ""); w.Code != 204 {
		t.Fatalf("DELETE: %d", w.Code)
	}
	if w = apiCall(h, "GET", "/api/events", ""); strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("empty calendar: %s", w.Body)
	}
	if w = apiCall(h, "DELETE", "/api/events/"+e.ID, ""); w.Code != 404 {
		t.Fatalf("missing delete: %d", w.Code)
	}
}

func TestAPIRejectsBadRequestsAndCrossOrigin(t *testing.T) {
	h := testHandler(t)
	e := testEvent()
	e.Date = "2026-02-30"
	invalidDate, _ := json.Marshal(e)
	for _, tt := range []struct {
		body string
		code int
	}{
		{"{", 400},
		{`{"unknown":true}`, 400},
		{string(invalidDate), 422},
		{`{} {}`, 400},
		{strings.Repeat(" ", 33<<10), 413},
	} {
		if w := apiCall(h, "POST", "/api/events", tt.body); w.Code != tt.code {
			t.Errorf("body %.40q: expected %d, got %d", tt.body, tt.code, w.Code)
		}
	}
	body, _ := json.Marshal(testEvent())
	for _, origin := range []string{"https://other.example", "null"} {
		r := httptest.NewRequest("POST", "http://127.0.0.1:8080/api/events", strings.NewReader(string(body)))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Errorf("cross origin %q allowed: %d", origin, w.Code)
		}
	}
	r := httptest.NewRequest("POST", "/api/events", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 415 {
		t.Fatalf("missing JSON content type: %d", w.Code)
	}
}

func TestEmbeddedFrontend(t *testing.T) {
	h := testHandler(t)
	for _, path := range []string{"/", "/style.css", "/app.js", "/favicon.svg"} {
		w := apiCall(h, "GET", path, "")
		if w.Code != 200 || w.Body.Len() == 0 {
			t.Errorf("asset %s: %d", path, w.Code)
		}
		if w.Header().Get("Content-Security-Policy") == "" {
			t.Error("missing CSP")
		}
	}
	if w := apiCall(h, "GET", "/api/missing", ""); w.Code != 404 || !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("unknown API path: %d", w.Code)
	}
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
