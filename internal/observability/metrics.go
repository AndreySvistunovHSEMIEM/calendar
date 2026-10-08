package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Metrics struct {
	Registry *prometheus.Registry
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	Jobs     *prometheus.CounterVec
}

func New() *Metrics {
	r := prometheus.NewRegistry()
	m := &Metrics{Registry: r, requests: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "orbita_http_requests_total", Help: "HTTP responses by route, method and code"}, []string{"route", "method", "code"}), duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "orbita_http_request_duration_seconds", Help: "HTTP latency", Buckets: prometheus.DefBuckets}, []string{"route"}), Jobs: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "orbita_jobs_total", Help: "Background processing results"}, []string{"service", "outcome"})}
	r.MustRegister(m.requests, m.duration, m.Jobs, prometheus.NewGoCollector(), prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	return m
}
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{})
}

type response struct {
	http.ResponseWriter
	status int
}

func (w *response) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
		w.ResponseWriter.WriteHeader(code)
	}
}
func (w *response) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(p)
}
func (w *response) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (m *Metrics) Wrap(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &response{ResponseWriter: w}
		next.ServeHTTP(rw, r)
		if rw.status == 0 {
			rw.status = 200
		}
		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		if strings.Contains(route, "{id}") {
			route = "/api/events/{id}"
		}
		m.requests.WithLabelValues(route, r.Method, strconv.Itoa(rw.status)).Inc()
		m.duration.WithLabelValues(route).Observe(time.Since(start).Seconds())
		if r.URL.Path != "/metrics" {
			logger.Info("http request", "method", r.Method, "route", route, "status", rw.status, "duration_ms", time.Since(start).Milliseconds())
		}
	})
}
