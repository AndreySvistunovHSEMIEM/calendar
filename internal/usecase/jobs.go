package usecase

import (
	"context"
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/domain"
	"time"
)

type StatusRepository interface {
	ApplyStatus(context.Context, domain.Job) (bool, error)
}
type StatusService struct{ repo StatusRepository }

func NewStatus(repo StatusRepository) *StatusService { return &StatusService{repo} }
func (s *StatusService) Apply(ctx context.Context, job domain.Job) (bool, error) {
	if job.Status != "ready" {
		return false, domain.Invalid("unsupported status")
	}
	return s.repo.ApplyStatus(ctx, job)
}

type StatusPublisher interface {
	PublishStatus(context.Context, domain.Job) error
}
type EventWorker struct {
	publisher StatusPublisher
	delay     time.Duration
}

func NewWorker(publisher StatusPublisher, delay time.Duration) *EventWorker {
	return &EventWorker{publisher, delay}
}
func (w *EventWorker) Process(ctx context.Context, job domain.Job) error {
	timer := time.NewTimer(w.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	job.Status = "ready"
	return w.publisher.PublishStatus(ctx, job)
}
