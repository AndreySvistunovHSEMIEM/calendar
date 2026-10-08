package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/domain"
	"github.com/AndreySvistunovHSEMIEM/calendar/internal/observability"
	appruntime "github.com/AndreySvistunovHSEMIEM/calendar/internal/runtime"
	"github.com/prometheus/client_golang/prometheus"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type account struct {
	token string
	ids   []string
}
type summary struct {
	Users             int       `json:"users"`
	Requests          int       `json:"requests"`
	Errors            int       `json:"errors"`
	DurationSeconds   float64   `json:"durationSeconds"`
	RequestsPerSecond float64   `json:"requestsPerSecond"`
	P50Millis         float64   `json:"p50Millis"`
	P95Millis         float64   `json:"p95Millis"`
	Timestamp         time.Time `json:"timestamp"`
}

func main() {
	base := flag.String("url", "http://127.0.0.1:8080", "target API")
	users := flag.Int("users", 20, "simulated users")
	rate := flag.Int("rate", 20, "total HTTP operations per second")
	duration := flag.Duration("duration", 30*time.Second, "benchmark duration")
	output := flag.String("output", "", "JSON report path")
	serve := flag.Bool("serve", false, "keep metrics available after the run")
	addr := flag.String("addr", ":8083", "metrics address")
	flag.Parse()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if *users < 1 || *users > 200 || *rate < 1 || *rate > 500 || *duration < time.Second || *duration > 5*time.Minute {
		logger.Error("use users 1..200, rate 1..500, duration 1s..5m")
		os.Exit(1)
	}
	root, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{MaxIdleConns: 200, MaxIdleConnsPerHost: 200}}
	accounts := make([]account, *users)
	prefix, err := domain.NewID()
	if err != nil {
		logger.Error("random identifier failed")
		os.Exit(1)
	}
	for i := range accounts {
		credentials := domain.Credentials{Name: fmt.Sprintf("Load user %d", i), Email: fmt.Sprintf("load-%s-%d@example.invalid", prefix, i), Password: prefix + "Test!"}
		body, _ := json.Marshal(credentials)
		request, err := http.NewRequestWithContext(root, "POST", strings.TrimRight(*base, "/")+"/api/auth/register", bytes.NewReader(body))
		if err != nil {
			logger.Error("invalid target URL")
			os.Exit(1)
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := client.Do(request)
		if err != nil {
			logger.Error("registration failed", "error", err)
			os.Exit(1)
		}
		var auth domain.AuthResult
		decodeErr := json.NewDecoder(response.Body).Decode(&auth)
		response.Body.Close()
		if response.StatusCode != 201 || decodeErr != nil || auth.Token == "" {
			logger.Error("registration rejected", "status", response.StatusCode)
			os.Exit(1)
		}
		accounts[i].token = auth.Token
	}
	metrics := observability.New()
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "orbita_load_requests_total", Help: "Load generator HTTP results"}, []string{"method", "code"})
	latency := prometheus.NewHistogram(prometheus.HistogramOpts{Name: "orbita_load_request_duration_seconds", Help: "Load generator HTTP latency", Buckets: prometheus.DefBuckets})
	metrics.Registry.MustRegister(counter, latency)
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", metrics.Handler())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	metricsCtx, stopMetrics := context.WithCancel(root)
	serverDone := make(chan error, 1)
	go func() { serverDone <- appruntime.Serve(metricsCtx, *addr, mux, logger) }()
	start := time.Now()
	ctx, stop := context.WithTimeout(root, *duration)
	defer stop()
	work := make(chan int, *users)
	var wg sync.WaitGroup
	var errorCount atomic.Int64
	var dataMu sync.Mutex
	durations := []float64{}
	for i := range accounts {
		wg.Add(1)
		go func(user *account) {
			defer wg.Done()
			for n := range work {
				method, path, expected := "GET", "/api/events", 200
				var payload []byte
				if n%3 == 0 {
					method = "POST"
					expected = 201
					payload, _ = json.Marshal(domain.Event{Title: "Нагрузочный тест", Date: time.Now().Format("2006-01-02"), Start: "10:00", End: "11:00", Category: "work"})
				} else if n%3 == 2 && len(user.ids) > 0 {
					method = "DELETE"
					path += "/" + user.ids[len(user.ids)-1]
					expected = 204
				}
				callCtx, cancel := context.WithTimeout(root, 10*time.Second)
				request, err := http.NewRequestWithContext(callCtx, method, strings.TrimRight(*base, "/")+path, bytes.NewReader(payload))
				if err != nil {
					cancel()
					errorCount.Add(1)
					continue
				}
				request.Header.Set("Authorization", "Bearer "+user.token)
				if payload != nil {
					request.Header.Set("Content-Type", "application/json")
				}
				begin := time.Now()
				response, err := client.Do(request)
				elapsed := time.Since(begin).Seconds()
				code := "network_error"
				if err == nil {
					code = fmt.Sprint(response.StatusCode)
					if response.StatusCode != expected {
						errorCount.Add(1)
					} else if method == "POST" {
						var e domain.Event
						if err = json.NewDecoder(response.Body).Decode(&e); err != nil {
							errorCount.Add(1)
						} else {
							user.ids = append(user.ids, e.ID)
						}
					} else if method == "DELETE" {
						user.ids = user.ids[:len(user.ids)-1]
					}
					io.Copy(io.Discard, response.Body)
					response.Body.Close()
				} else {
					errorCount.Add(1)
				}
				cancel()
				counter.WithLabelValues(method, code).Inc()
				latency.Observe(elapsed)
				dataMu.Lock()
				durations = append(durations, elapsed*1000)
				dataMu.Unlock()
			}
		}(&accounts[i])
	}
	ticker := time.NewTicker(time.Second / time.Duration(*rate))
	n := 0
schedule:
	for {
		select {
		case <-ctx.Done():
			break schedule
		case <-ticker.C:
			select {
			case work <- n:
				n++
			case <-ctx.Done():
				break schedule
			}
		}
	}
	ticker.Stop()
	close(work)
	wg.Wait()
	elapsed := time.Since(start).Seconds()
	sort.Float64s(durations)
	result := summary{Users: *users, Requests: len(durations), Errors: int(errorCount.Load()), DurationSeconds: elapsed, RequestsPerSecond: float64(len(durations)) / elapsed, Timestamp: time.Now().UTC()}
	if len(durations) > 0 {
		result.P50Millis = durations[(len(durations)-1)*50/100]
		result.P95Millis = durations[(len(durations)-1)*95/100]
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(data))
	if *output != "" {
		if err = os.MkdirAll(filepath.Dir(*output), 0750); err == nil {
			err = os.WriteFile(*output, append(data, '\n'), 0640)
		}
		if err != nil {
			logger.Error("cannot save report", "error", err)
		}
	}
	if *serve && root.Err() == nil && result.Errors == 0 {
		<-root.Done()
	}
	stopMetrics()
	if err = <-serverDone; err != nil {
		logger.Error("metrics server failed", "error", err)
	}
	if result.Errors > 0 {
		os.Exit(1)
	}
}
