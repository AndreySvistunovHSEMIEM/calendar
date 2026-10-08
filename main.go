package main

import (
	"context"
	"embed"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

//go:embed web/*
var frontend embed.FS

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP address")
	path := flag.String("data", "data/events.json", "Event storage file")
	demo := flag.Bool("demo", false, "Fill a new storage file with sample events")
	flag.Parse()
	store, err := openStore(*path, *demo, time.Now())
	if err != nil {
		log.Fatalf("Не удалось открыть календарь: %v", err)
	}
	web, err := fs.Sub(frontend, "web")
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{Addr: *addr, Handler: newHandler(store, web), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go func() {
		<-ctx.Done()
		shutdown, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if err := server.Shutdown(shutdown); err != nil {
			log.Printf("Остановка сервера: %v", err)
		}
	}()
	log.Printf("Орбита готова: http://%s · данные: %s", *addr, *path)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
