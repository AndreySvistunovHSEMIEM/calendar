package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/domain"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type storedUser struct {
	User domain.User `json:"user"`
	Hash string      `json:"passwordHash"`
}
type storedEvent struct {
	UserID   string       `json:"userId"`
	Event    domain.Event `json:"event"`
	Revision int64        `json:"revision"`
}
type probe struct {
	UserID string `json:"userId"`
	Value  string `json:"value"`
}
type fileState struct {
	Version  int              `json:"version"`
	Users    []storedUser     `json:"users"`
	Sessions []domain.Session `json:"sessions"`
	Events   []storedEvent    `json:"events"`
	Probes   []probe          `json:"probes"`
}
type JSON struct {
	mu    sync.RWMutex
	path  string
	state fileState
}

func OpenJSON(path string) (*JSON, error) {
	repo := &JSON{path: path, state: fileState{Version: 2}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return repo, repo.persist(repo.state)
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(data, &repo.state); err != nil {
		return nil, fmt.Errorf("storage is corrupt and has not been overwritten: %w", err)
	}
	if repo.state.Version != 2 {
		return nil, errors.New("unsupported storage version; use a new path for the authenticated calendar")
	}
	users, emails, ids := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, u := range repo.state.Users {
		if u.User.ID == "" || users[u.User.ID] || emails[u.User.Email] || u.Hash == "" {
			return nil, errors.New("invalid or duplicate user in storage")
		}
		users[u.User.ID] = true
		emails[u.User.Email] = true
	}
	for _, row := range repo.state.Events {
		e := row.Event
		if !users[row.UserID] || e.ID == "" || ids[e.ID] {
			return nil, errors.New("invalid or duplicate event in storage")
		}
		ids[e.ID] = true
		if err = domain.ValidateEvent(&e); err != nil {
			return nil, err
		}
	}
	return repo, nil
}
func (r *JSON) persist(state fileState) error {
	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".calendar-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(0600); err != nil {
		return err
	}
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(state); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), r.path)
}
func (r *JSON) commit(state fileState) error {
	if err := r.persist(state); err != nil {
		return err
	}
	r.state = state
	return nil
}
func (r *JSON) Test(context.Context) (string, error) { return "Hello!", nil }
func (r *JSON) Ready(context.Context) error          { return nil }
func (r *JSON) Probe(ctx context.Context, user, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	next := r.state
	next.Probes = append(append([]probe{}, r.state.Probes...), probe{user, value})
	return r.commit(next)
}
func (r *JSON) CreateUser(ctx context.Context, user domain.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, u := range r.state.Users {
		if u.User.Email == user.Email {
			return domain.ErrConflict
		}
	}
	next := r.state
	next.Users = append(append([]storedUser{}, r.state.Users...), storedUser{user, user.PasswordHash})
	return r.commit(next)
}
func (r *JSON) UserByEmail(ctx context.Context, email string) (domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, u := range r.state.Users {
		if u.User.Email == email {
			user := u.User
			user.PasswordHash = u.Hash
			return user, nil
		}
	}
	return domain.User{}, domain.ErrNotFound
}
func (r *JSON) UserByID(ctx context.Context, id string) (domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, u := range r.state.Users {
		if u.User.ID == id {
			user := u.User
			user.PasswordHash = u.Hash
			return user, nil
		}
	}
	return domain.User{}, domain.ErrNotFound
}
func (r *JSON) SaveSession(ctx context.Context, session domain.Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	next := r.state
	next.Sessions = []domain.Session{}
	for _, s := range r.state.Sessions {
		if s.ExpiresAt.After(time.Now()) {
			next.Sessions = append(next.Sessions, s)
		}
	}
	next.Sessions = append(next.Sessions, session)
	return r.commit(next)
}
func (r *JSON) Session(ctx context.Context, id string) (domain.Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, s := range r.state.Sessions {
		if s.ID == id {
			return s, nil
		}
	}
	return domain.Session{}, domain.ErrNotFound
}
func (r *JSON) DeleteSession(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	next := r.state
	next.Sessions = []domain.Session{}
	for _, s := range r.state.Sessions {
		if s.ID != id {
			next.Sessions = append(next.Sessions, s)
		}
	}
	return r.commit(next)
}
func (r *JSON) ListEvents(ctx context.Context, user string) ([]domain.Event, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	events := []domain.Event{}
	for _, row := range r.state.Events {
		if row.UserID == user {
			e := row.Event
			e.Revision = row.Revision
			events = append(events, e)
		}
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].Date != events[j].Date {
			return events[i].Date < events[j].Date
		}
		return events[i].Start < events[j].Start
	})
	return events, nil
}
func (r *JSON) SaveEvent(ctx context.Context, user string, e domain.Event, id string, async bool) (domain.Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	next := r.state
	next.Events = append([]storedEvent{}, r.state.Events...)
	index := -1
	e.Revision = 1
	if id != "" {
		for i, row := range next.Events {
			if row.Event.ID == id && row.UserID == user {
				index = i
				e.Revision = row.Revision + 1
				break
			}
		}
		if index < 0 {
			return domain.Event{}, domain.ErrNotFound
		}
		e.ID = id
	} else {
		var err error
		e.ID, err = domain.NewID()
		if err != nil {
			return domain.Event{}, err
		}
	}
	e.ProcessingStatus = "ready"
	row := storedEvent{user, e, e.Revision}
	if index < 0 {
		next.Events = append(next.Events, row)
	} else {
		next.Events[index] = row
	}
	if err := r.commit(next); err != nil {
		return domain.Event{}, err
	}
	return e, nil
}
func (r *JSON) DeleteEvent(ctx context.Context, user, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	next := r.state
	next.Events = []storedEvent{}
	found := false
	for _, row := range r.state.Events {
		if row.UserID == user && row.Event.ID == id {
			found = true
			continue
		}
		next.Events = append(next.Events, row)
	}
	if !found {
		return domain.ErrNotFound
	}
	return r.commit(next)
}
