package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func authenticatedCall(h http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestAuthenticationOwnershipAndRevocation(t *testing.T) {
	h, repo := rawHandler(t)
	first := register(t, h, "first@example.invalid")
	second := register(t, h, "second@example.invalid")
	if w := authenticatedCall(h, "GET", "/api/events", "", ""); w.Code != 401 {
		t.Fatalf("unauthenticated list: %d", w.Code)
	}
	if w := authenticatedCall(h, "GET", "/api/events", "", first.Token+"tampered"); w.Code != 401 {
		t.Fatalf("forged token accepted: %d", w.Code)
	}
	if w := apiCall(h, "POST", "/api/auth/register", `{"name":"Duplicate","email":"first@example.invalid","password":"TestPassword123!"}`); w.Code != 409 {
		t.Fatalf("duplicate registration: %d", w.Code)
	}
	if w := apiCall(h, "POST", "/api/auth/login", `{"email":"first@example.invalid","password":"WrongPassword123!"}`); w.Code != 401 {
		t.Fatalf("incorrect password accepted: %d", w.Code)
	}
	user, err := repo.UserByEmail(context.Background(), "first@example.invalid")
	if err != nil || !strings.HasPrefix(user.PasswordHash, "$2") {
		t.Fatal("password was not hashed")
	}
	body, _ := json.Marshal(testEvent())
	w := authenticatedCall(h, "POST", "/api/events", string(body), first.Token)
	if w.Code != 201 {
		t.Fatal(w.Body)
	}
	var event Event
	json.Unmarshal(w.Body.Bytes(), &event)
	if w = authenticatedCall(h, "GET", "/api/events", "", second.Token); strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatal("another user's events leaked")
	}
	body, _ = json.Marshal(event)
	for _, method := range []string{"PUT", "DELETE"} {
		if w = authenticatedCall(h, method, "/api/events/"+event.ID, string(body), second.Token); w.Code != 404 {
			t.Fatalf("cross-user %s: %d", method, w.Code)
		}
	}
	if w = authenticatedCall(h, "GET", "/api/export", "", second.Token); strings.Contains(w.Body.String(), event.Title) {
		t.Fatal("another user's event exported")
	}
	r := httptest.NewRequest("POST", "/dbtest", strings.NewReader("database write probe"))
	r.Header.Set("Authorization", "Bearer "+first.Token)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 201 {
		t.Fatalf("dbtest: %d", w.Code)
	}
	if w = authenticatedCall(h, "POST", "/api/auth/logout", "", first.Token); w.Code != 204 {
		t.Fatal("logout failed")
	}
	if w = authenticatedCall(h, "GET", "/api/events", "", first.Token); w.Code != 401 {
		t.Fatal("revoked session still works")
	}
	if w = authenticatedCall(h, "GET", "/api/events", "", second.Token); w.Code != 200 {
		t.Fatal("logout revoked another user's session")
	}
}
