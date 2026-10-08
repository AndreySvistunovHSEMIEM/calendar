package usecase

import (
	"context"
	"errors"
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/domain"
	"strings"
	"time"
)

// Repository is defined by the consuming layer. Implementations may use SQL,
// files or in-memory state; use cases know none of those details.
type Repository interface {
	Test(context.Context) (string, error)
	Probe(context.Context, string, string) error
	Ready(context.Context) error
	CreateUser(context.Context, domain.User) error
	UserByEmail(context.Context, string) (domain.User, error)
	UserByID(context.Context, string) (domain.User, error)
	SaveSession(context.Context, domain.Session) error
	Session(context.Context, string) (domain.Session, error)
	DeleteSession(context.Context, string) error
	ListEvents(context.Context, string) ([]domain.Event, error)
	SaveEvent(context.Context, string, domain.Event, string, bool) (domain.Event, error)
	DeleteEvent(context.Context, string, string) error
}
type Passwords interface {
	Hash(string) (string, error)
	Verify(string, string) bool
}
type Tokens interface {
	Issue(domain.User, domain.Session) (string, error)
	Verify(string) (domain.Session, error)
}

// Calendar is the interface consumed by the HTTP delivery layer.
type Calendar interface {
	Test(context.Context) (string, error)
	Probe(context.Context, string, string) error
	Ready(context.Context) error
	Register(context.Context, domain.Credentials) (domain.AuthResult, error)
	Login(context.Context, domain.Credentials) (domain.AuthResult, error)
	Authenticate(context.Context, string) (domain.User, domain.Session, error)
	Logout(context.Context, string) error
	List(context.Context, string) ([]domain.Event, error)
	Save(context.Context, string, domain.Event, string) (domain.Event, error)
	Delete(context.Context, string, string) error
}
type Service struct {
	repo      Repository
	passwords Passwords
	tokens    Tokens
	ttl       time.Duration
	async     bool
}

func New(repo Repository, passwords Passwords, tokens Tokens, ttl time.Duration, async bool) *Service {
	return &Service{repo, passwords, tokens, ttl, async}
}
func (s *Service) Test(ctx context.Context) (string, error) { return s.repo.Test(ctx) }
func (s *Service) Ready(ctx context.Context) error          { return s.repo.Ready(ctx) }
func (s *Service) Probe(ctx context.Context, user, value string) error {
	if strings.TrimSpace(value) == "" || len(value) > 4096 {
		return domain.Invalid("Строка должна содержать от 1 до 4096 байт")
	}
	return s.repo.Probe(ctx, user, value)
}
func (s *Service) Register(ctx context.Context, c domain.Credentials) (domain.AuthResult, error) {
	if err := domain.NormalizeCredentials(&c, true); err != nil {
		return domain.AuthResult{}, err
	}
	hash, err := s.passwords.Hash(c.Password)
	if err != nil {
		return domain.AuthResult{}, err
	}
	id, err := domain.NewID()
	if err != nil {
		return domain.AuthResult{}, err
	}
	user := domain.User{ID: id, Name: c.Name, Email: c.Email, PasswordHash: hash}
	if err = s.repo.CreateUser(ctx, user); err != nil {
		return domain.AuthResult{}, err
	}
	return s.session(ctx, user)
}
func (s *Service) Login(ctx context.Context, c domain.Credentials) (domain.AuthResult, error) {
	if err := domain.NormalizeCredentials(&c, false); err != nil {
		return domain.AuthResult{}, domain.ErrUnauthorized
	}
	user, err := s.repo.UserByEmail(ctx, c.Email)
	if errors.Is(err, domain.ErrNotFound) {
		s.passwords.Verify("", c.Password)
		return domain.AuthResult{}, domain.ErrUnauthorized
	}
	if err != nil {
		return domain.AuthResult{}, err
	}
	if !s.passwords.Verify(user.PasswordHash, c.Password) {
		return domain.AuthResult{}, domain.ErrUnauthorized
	}
	return s.session(ctx, user)
}
func (s *Service) session(ctx context.Context, user domain.User) (domain.AuthResult, error) {
	id, err := domain.NewID()
	if err != nil {
		return domain.AuthResult{}, err
	}
	session := domain.Session{ID: id, UserID: user.ID, ExpiresAt: time.Now().UTC().Add(s.ttl)}
	token, err := s.tokens.Issue(user, session)
	if err != nil {
		return domain.AuthResult{}, err
	}
	if err = s.repo.SaveSession(ctx, session); err != nil {
		return domain.AuthResult{}, err
	}
	return domain.AuthResult{User: user, Token: token, ExpiresAt: session.ExpiresAt}, nil
}
func (s *Service) Authenticate(ctx context.Context, token string) (domain.User, domain.Session, error) {
	claimed, err := s.tokens.Verify(token)
	if err != nil {
		return domain.User{}, domain.Session{}, domain.ErrUnauthorized
	}
	stored, err := s.repo.Session(ctx, claimed.ID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.User{}, domain.Session{}, domain.ErrUnauthorized
	}
	if err != nil {
		return domain.User{}, domain.Session{}, err
	}
	if stored.UserID != claimed.UserID || !stored.ExpiresAt.After(time.Now()) {
		return domain.User{}, domain.Session{}, domain.ErrUnauthorized
	}
	user, err := s.repo.UserByID(ctx, stored.UserID)
	if errors.Is(err, domain.ErrNotFound) {
		err = domain.ErrUnauthorized
	}
	return user, stored, err
}
func (s *Service) Logout(ctx context.Context, id string) error { return s.repo.DeleteSession(ctx, id) }
func (s *Service) List(ctx context.Context, user string) ([]domain.Event, error) {
	return s.repo.ListEvents(ctx, user)
}
func (s *Service) Save(ctx context.Context, user string, e domain.Event, id string) (domain.Event, error) {
	if err := domain.ValidateEvent(&e); err != nil {
		return domain.Event{}, err
	}
	return s.repo.SaveEvent(ctx, user, e, id, s.async)
}
func (s *Service) Delete(ctx context.Context, user, id string) error {
	return s.repo.DeleteEvent(ctx, user, id)
}
