package security

import (
	"errors"
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/domain"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"time"
)

type Passwords struct {
	cost  int
	dummy string
}

func NewPasswords(cost int) (*Passwords, error) {
	dummy, err := bcrypt.GenerateFromPassword([]byte("timing-only-dummy-password"), cost)
	return &Passwords{cost, string(dummy)}, err
}
func (p *Passwords) Hash(value string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(value), p.cost)
	return string(hash), err
}
func (p *Passwords) Verify(hash, value string) bool {
	if hash == "" {
		hash = p.dummy
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(value)) == nil
}

type Tokens struct{ secret []byte }

func NewTokens(secret string) (*Tokens, error) {
	if len(secret) < 32 {
		return nil, errors.New("JWT_SECRET must contain at least 32 bytes")
	}
	return &Tokens{[]byte(secret)}, nil
}
func (t *Tokens) Issue(user domain.User, session domain.Session) (string, error) {
	claims := jwt.RegisteredClaims{Issuer: "orbita", Audience: jwt.ClaimStrings{"orbita-calendar"}, Subject: user.ID, ID: session.ID, IssuedAt: jwt.NewNumericDate(time.Now()), ExpiresAt: jwt.NewNumericDate(session.ExpiresAt)}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
}
func (t *Tokens) Verify(value string) (domain.Session, error) {
	var claims jwt.RegisteredClaims
	token, err := jwt.ParseWithClaims(value, &claims, func(token *jwt.Token) (any, error) { return t.secret, nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer("orbita"), jwt.WithAudience("orbita-calendar"), jwt.WithExpirationRequired(), jwt.WithIssuedAt())
	if err != nil || !token.Valid || claims.ID == "" || claims.Subject == "" {
		return domain.Session{}, domain.ErrUnauthorized
	}
	return domain.Session{ID: claims.ID, UserID: claims.Subject, ExpiresAt: claims.ExpiresAt.Time}, nil
}
