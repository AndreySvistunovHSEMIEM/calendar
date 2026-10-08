package repository

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schema string

type Postgres struct{ pool *pgxpool.Pool }

func OpenPostgres(ctx context.Context, url string) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	r := &Postgres{pool}
	if err = r.Ready(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if _, err = pool.Exec(ctx, schema); err != nil {
		pool.Close()
		return nil, err
	}
	return r, nil
}
func (r *Postgres) Close()                          { r.pool.Close() }
func (r *Postgres) Ready(ctx context.Context) error { return r.pool.Ping(ctx) }
func (r *Postgres) Test(ctx context.Context) (string, error) {
	var value string
	err := r.pool.QueryRow(ctx, "SELECT 'Hello!'").Scan(&value)
	return value, err
}
func (r *Postgres) Probe(ctx context.Context, user, value string) error {
	_, err := r.pool.Exec(ctx, "INSERT INTO database_probes(user_id,value) VALUES($1,$2)", user, value)
	return err
}
func (r *Postgres) CreateUser(ctx context.Context, u domain.User) error {
	_, err := r.pool.Exec(ctx, "INSERT INTO users(id,name,email,password_hash) VALUES($1,$2,$3,$4)", u.ID, u.Name, u.Email, u.PasswordHash)
	var p *pgconn.PgError
	if errors.As(err, &p) && p.Code == "23505" {
		return domain.ErrConflict
	}
	return err
}
func scanUser(row pgx.Row) (domain.User, error) {
	var u domain.User
	err := row.Scan(&u.ID, &u.Name, &u.Email, &u.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrNotFound
	}
	return u, err
}
func (r *Postgres) UserByEmail(ctx context.Context, email string) (domain.User, error) {
	return scanUser(r.pool.QueryRow(ctx, "SELECT id,name,email,password_hash FROM users WHERE email=$1", email))
}
func (r *Postgres) UserByID(ctx context.Context, id string) (domain.User, error) {
	return scanUser(r.pool.QueryRow(ctx, "SELECT id,name,email,password_hash FROM users WHERE id=$1", id))
}
func (r *Postgres) SaveSession(ctx context.Context, s domain.Session) error {
	_, err := r.pool.Exec(ctx, "INSERT INTO sessions(id,user_id,expires_at) VALUES($1,$2,$3)", s.ID, s.UserID, s.ExpiresAt)
	return err
}
func (r *Postgres) Session(ctx context.Context, id string) (domain.Session, error) {
	var s domain.Session
	err := r.pool.QueryRow(ctx, "SELECT id,user_id,expires_at FROM sessions WHERE id=$1", id).Scan(&s.ID, &s.UserID, &s.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrNotFound
	}
	return s, err
}
func (r *Postgres) DeleteSession(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, "DELETE FROM sessions WHERE id=$1", id)
	return err
}
func (r *Postgres) ListEvents(ctx context.Context, user string) ([]domain.Event, error) {
	rows, err := r.pool.Query(ctx, "SELECT payload,status,revision FROM calendar_events WHERE user_id=$1 ORDER BY payload->>'date',payload->>'start',id", user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []domain.Event{}
	for rows.Next() {
		var e domain.Event
		var raw []byte
		var status string
		var rev int64
		if err = rows.Scan(&raw, &status, &rev); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &e); err != nil {
			return nil, err
		}
		e.ProcessingStatus = status
		e.Revision = rev
		result = append(result, e)
	}
	return result, rows.Err()
}
func (r *Postgres) SaveEvent(ctx context.Context, user string, e domain.Event, id string, async bool) (domain.Event, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Event{}, err
	}
	defer tx.Rollback(ctx)
	e.ProcessingStatus = "ready"
	if async {
		e.ProcessingStatus = "pending"
	}
	e.Revision = 1
	if id == "" {
		e.ID, err = domain.NewID()
		if err != nil {
			return domain.Event{}, err
		}
	} else {
		e.ID = id
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return domain.Event{}, err
	}
	if id == "" {
		_, err = tx.Exec(ctx, "INSERT INTO calendar_events(id,user_id,payload,status) VALUES($1,$2,$3,$4)", e.ID, user, raw, e.ProcessingStatus)
	} else {
		err = tx.QueryRow(ctx, "UPDATE calendar_events SET payload=$3,status=$4,revision=revision+1 WHERE id=$1 AND user_id=$2 RETURNING revision", id, user, raw, e.ProcessingStatus).Scan(&e.Revision)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Event{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Event{}, err
	}
	if async {
		jobID, err := domain.NewID()
		if err != nil {
			return domain.Event{}, err
		}
		job := domain.Job{ID: jobID, UserID: user, EventID: e.ID, Revision: e.Revision}
		raw, err = json.Marshal(job)
		if err != nil {
			return domain.Event{}, err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO event_outbox(id,payload) VALUES($1,$2)", job.ID, raw); err != nil {
			return domain.Event{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Event{}, err
	}
	return e, nil
}
func (r *Postgres) DeleteEvent(ctx context.Context, user, id string) error {
	result, err := r.pool.Exec(ctx, "DELETE FROM calendar_events WHERE id=$1 AND user_id=$2", id, user)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// Claiming leases lets a publisher crash without losing jobs, and prevents two
// dispatchers from publishing the same row concurrently. Delivery is at least once.
func (r *Postgres) ClaimJobs(ctx context.Context) ([]domain.Job, error) {
	rows, err := r.pool.Query(ctx, `WITH pending AS (SELECT id FROM event_outbox WHERE NOT published AND (lease_until IS NULL OR lease_until<now()) ORDER BY created_at LIMIT 25 FOR UPDATE SKIP LOCKED) UPDATE event_outbox SET lease_until=now()+interval '30 seconds' FROM pending WHERE event_outbox.id=pending.id RETURNING event_outbox.payload`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []domain.Job{}
	for rows.Next() {
		var data []byte
		var job domain.Job
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &job); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}
func (r *Postgres) MarkPublished(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, "UPDATE event_outbox SET published=true,lease_until=NULL WHERE id=$1", id)
	return err
}
func (r *Postgres) ApplyStatus(ctx context.Context, job domain.Job) (bool, error) {
	result, err := r.pool.Exec(ctx, "UPDATE calendar_events SET status='ready' WHERE id=$1 AND user_id=$2 AND revision=$3 AND status='pending'", job.EventID, job.UserID, job.Revision)
	return err == nil && result.RowsAffected() > 0, err
}
