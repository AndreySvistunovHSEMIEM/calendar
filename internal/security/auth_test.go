package security

import (
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/domain"
	"github.com/golang-jwt/jwt/v5"
	"strings"
	"testing"
	"time"
)

func TestJWTRejectsExpiredWrongAlgorithmAndWrongAudience(t *testing.T) {
	secret := strings.Repeat("strong-test-key", 3)
	tokens, err := NewTokens(secret)
	if err != nil {
		t.Fatal(err)
	}
	user := domain.User{ID: "owner"}
	session := domain.Session{ID: "session", UserID: user.ID, ExpiresAt: time.Now().Add(-time.Minute)}
	expired, err := tokens.Issue(user, session)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tokens.Verify(expired); err == nil {
		t.Fatal("expired token accepted")
	}
	for _, claims := range []jwt.RegisteredClaims{{Issuer: "orbita", Audience: jwt.ClaimStrings{"other-service"}, Subject: "owner", ID: "session", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}, {Issuer: "another-issuer", Audience: jwt.ClaimStrings{"orbita-calendar"}, Subject: "owner", ID: "session", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}} {
		value, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
		if _, err = tokens.Verify(value); err == nil {
			t.Fatal("foreign claims accepted")
		}
	}
	claims := jwt.RegisteredClaims{Issuer: "orbita", Audience: jwt.ClaimStrings{"orbita-calendar"}, Subject: "owner", ID: "session", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}
	value, _ := jwt.NewWithClaims(jwt.SigningMethodHS384, claims).SignedString([]byte(secret))
	if _, err = tokens.Verify(value); err == nil {
		t.Fatal("unexpected signing algorithm accepted")
	}
}
