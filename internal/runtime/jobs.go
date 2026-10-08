package runtime

import (
	"context"
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/broker"
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/domain"
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/observability"
	"log/slog"
	"time"
)

type JobsRepository interface {
	ClaimJobs(context.Context) ([]domain.Job, error)
	MarkPublished(context.Context, string) error
}
type Publisher interface {
	Send(context.Context, string, domain.Job) error
}

func Dispatch(ctx context.Context, repo JobsRepository, publisher Publisher, metrics *observability.Metrics, logger *slog.Logger) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			batch, cancel := context.WithTimeout(ctx, 20*time.Second)
			jobs, err := repo.ClaimJobs(batch)
			if err != nil {
				cancel()
				if ctx.Err() == nil {
					logger.Error("outbox claim failed", "error", err)
				}
				continue
			}
			for _, job := range jobs {
				if err = publisher.Send(batch, broker.NewEvents, job); err == nil {
					err = repo.MarkPublished(batch, job.ID)
				}
				if err != nil {
					metrics.Jobs.WithLabelValues("api", "publish_error").Inc()
					logger.Error("outbox publish deferred", "job", job.ID, "error", err)
				} else {
					metrics.Jobs.WithLabelValues("api", "published").Inc()
				}
			}
			cancel()
		}
	}
}

type StatusUsecase interface {
	Apply(context.Context, domain.Job) (bool, error)
}

func ApplyStatus(service StatusUsecase, metrics *observability.Metrics) func(context.Context, domain.Job) error {
	return func(ctx context.Context, job domain.Job) error {
		changed, err := service.Apply(ctx, job)
		if err != nil {
			return err
		}
		outcome := "stale_or_duplicate"
		if changed {
			outcome = "ready"
		}
		metrics.Jobs.WithLabelValues("api", outcome).Inc()
		return nil
	}
}
