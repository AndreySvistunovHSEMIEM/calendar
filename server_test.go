package main

import (
	"encoding/json"
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/domain"
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/repository"
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/security"
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/transport"
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/usecase"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func rawHandler(t *testing.T) (http.Handler, *repository.JSON) {
	t.Helper()
	repo, err := repository.OpenJSON(filepath.Join(t.TempDir(), "calendar.json"))
	if err != nil {
		t.Fatal(err)
	}
	passwords, err := security.NewPasswords(4)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := security.NewTokens(strings.Repeat("test-secret", 4))
	if err != nil {
		t.Fatal(err)
	}
	web, err := fs.Sub(frontend, "web")
	if err != nil {
		t.Fatal(err)
	}
	service := usecase.New(repo, passwords, tokens, time.Hour, false)
	return transport.New(service, web, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), false), repo
}
func register(t *testing.T, h http.Handler, email string) domain.AuthResult {
	t.Helper()
	body, _ := json.Marshal(domain.Credentials{Name: "Test user", Email: email, Password: "TestPassword123!"})
	w := apiCall(h, "POST", "/api/auth/register", string(body))
	if w.Code != 201 {
		t.Fatalf("register: %d %s", w.Code, w.Body)
	}
	var result domain.AuthResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "orbita_session" && (!cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode) {
			t.Fatal("unsafe session cookie")
		}
	}
	if strings.Contains(w.Body.String(), "passwordHash") || strings.Contains(w.Body.String(), "$2a$") {
		t.Fatal("password leaked")
	}
	return result
}
func testHandler(t *testing.T) http.Handler {
	h, _ := rawHandler(t)
	user := register(t, h, "one@example.invalid")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+user.Token)
		h.ServeHTTP(w, r)
	})
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
