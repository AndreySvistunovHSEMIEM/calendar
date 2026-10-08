package main

import (
	"context"
	"errors"
	"flag"
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/broker"
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/domain"
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/observability"
	appruntime "github.com/AndreySvistunovHSEMIEM/calendar/internal/runtime"
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/usecase"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

func main() {
	addr := flag.String("addr", ":8082", "metrics address")
	workers := flag.Int("workers", 4, "fixed worker count")
	delay := flag.Duration("delay", 500*time.Millisecond, "simulated event preparation")
	flag.Parse()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if *workers < 1 || *workers > 64 || *delay < 0 || *delay > 5*time.Second {
		logger.Error("workers must be between 1 and 64")
		os.Exit(1)
	}
	url := os.Getenv("RABBITMQ_URL")
	if url == "" {
		logger.Error("RABBITMQ_URL is required")
		os.Exit(1)
	}
	rabbit := broker.New(url, logger)
	defer rabbit.Close()
	metrics := observability.New()
	processor := usecase.NewWorker(rabbit, *delay)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		rabbit.Listen(ctx, broker.NewEvents, *workers, func(ctx context.Context, job domain.Job) error {
			if err := processor.Process(ctx, job); err != nil {
				metrics.Jobs.WithLabelValues("worker", "error").Inc()
				return err
			}
			metrics.Jobs.WithLabelValues("worker", "processed").Inc()
			return nil
		})
	}()
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", metrics.Handler())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := rabbit.Ready(r.Context()); err != nil {
			http.Error(w, "broker unavailable", 503)
			return
		}
		w.Write([]byte("ready"))
	})
	err := appruntime.Serve(ctx, *addr, mux, logger)
	cancel()
	wg.Wait()
	if err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("worker stopped", "error", err)
		os.Exit(1)
	}
}
