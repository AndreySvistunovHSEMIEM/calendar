package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"errors"
	"flag"
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/broker"
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/domain"
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/observability"
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/repository"
	appruntime "github.com/AndreySvistunovHSEMIEM/calendar/internal/runtime"
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/security"
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/transport"
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/usecase"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

//go:embed web/*
var frontend embed.FS

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func localSecret(path string) (string, error) {
	if data, err := os.ReadFile(path); err == nil {
		return string(data), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	secret := hex.EncodeToString(bytes[:])
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err = f.WriteString(secret); err != nil {
		return "", err
	}
	if err = f.Sync(); err != nil {
		return "", err
	}
	return secret, nil
}
func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("calendar stopped", "error", err)
		os.Exit(1)
	}
}
func run(logger *slog.Logger) error {
	defaultStorage := "json"
	if os.Getenv("DATABASE_URL") != "" {
		defaultStorage = "postgres"
	}
	addr := flag.String("addr", env("HTTP_ADDR", "127.0.0.1:8080"), "HTTP address")
	path := flag.String("data", "data/calendar.json", "local JSON storage path")
	storage := flag.String("storage", env("STORAGE", defaultStorage), "postgres or json")
	demo := flag.Bool("demo", os.Getenv("DEMO_ENABLED") == "true", "create a demo account on a new installation")
	flag.Parse()
	secret := os.Getenv("JWT_SECRET")
	if secret == "" && *storage == "json" {
		var err error
		secret, err = localSecret(*path + ".key")
		if err != nil {
			return err
		}
	}
	tokens, err := security.NewTokens(secret)
	if err != nil {
		return err
	}
	passwords, err := security.NewPasswords(10)
	if err != nil {
		return err
	}
	ttl, err := time.ParseDuration(env("SESSION_TTL", "1h"))
	if err != nil || ttl < time.Minute || ttl > 7*24*time.Hour {
		return errors.New("SESSION_TTL must be between 1m and 168h")
	}
	var repo usecase.Repository
	var pg *repository.Postgres
	switch *storage {
	case "postgres":
		if os.Getenv("DATABASE_URL") == "" {
			return errors.New("DATABASE_URL is required for PostgreSQL")
		}
		startup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		pg, err = repository.OpenPostgres(startup, os.Getenv("DATABASE_URL"))
		if err != nil {
			return err
		}
		defer pg.Close()
		repo = pg
	case "json":
		repo, err = repository.OpenJSON(*path)
		if err != nil {
			return err
		}
	default:
		return errors.New("STORAGE must be postgres or json")
	}
	rabbitURL := os.Getenv("RABBITMQ_URL")
	async := pg != nil && rabbitURL != ""
	service := usecase.New(repo, passwords, tokens, ttl, async)
	if *demo {
		if _, err = repo.UserByEmail(context.Background(), "demo@orbita.local"); errors.Is(err, domain.ErrNotFound) {
			result, err := service.Register(context.Background(), domain.Credentials{Name: "Космический путешественник", Email: "demo@orbita.local", Password: "OrbitaDemo2026!"})
			if err != nil {
				return err
			}
			for _, event := range domain.DemoEvents(time.Now()) {
				if _, err = service.Save(context.Background(), result.User.ID, event, ""); err != nil {
					return err
				}
			}
		} else if err != nil {
			return err
		}
	}
	web, err := fs.Sub(frontend, "web")
	if err != nil {
		return err
	}
	metrics := observability.New()
	handler := metrics.Wrap(transport.New(service, web, metrics.Handler(), logger, os.Getenv("COOKIE_SECURE") == "true"), logger)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	bg, bgCancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	if async {
		rabbit := broker.New(rabbitURL, logger)
		defer rabbit.Close()
		wg.Add(2)
		go func() { defer wg.Done(); appruntime.Dispatch(bg, pg, rabbit, metrics, logger) }()
		go func() {
			defer wg.Done()
			rabbit.Listen(bg, broker.Statuses, 4, appruntime.ApplyStatus(usecase.NewStatus(pg), metrics))
		}()
	}
	logger.Info("calendar configuration", "storage", *storage, "asynchronous_processing", async)
	err = appruntime.Serve(ctx, *addr, handler, logger)
	bgCancel()
	wg.Wait()
	return err
}
