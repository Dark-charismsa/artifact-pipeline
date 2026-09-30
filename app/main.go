package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// version is set at build time: -ldflags "-X main.version=<git sha>".
var version = "dev"

// Metrics. Prometheus scrapes these from /metrics. The canary analysis
// computes the error rate from http_requests_total.
var (
	requests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Requests handled, by path and HTTP status code.",
	}, []string{"path", "code"})

	duration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "Request latency in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"path"})

	info = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "app_info",
		Help: "Always 1. The version label says which build is running.",
	}, []string{"version"})
)

// config is read once at startup. Changing it means restarting the pod,
// which is how a "bad release" is simulated: same image, different env.
type config struct {
	port      string
	errorRate float64 // fraction of requests to "/" that fail with 500
	latency   time.Duration
}

func loadConfig() config {
	return config{
		port:      env("PORT", "8080"),
		errorRate: envFloat("ERROR_RATE", 0),
		latency:   time.Duration(envFloat("LATENCY_MS", 0)) * time.Millisecond,
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envFloat(key string, fallback float64) float64 {
	v, err := strconv.ParseFloat(os.Getenv(key), 64)
	if err != nil {
		return fallback
	}
	return v
}

// statusRecorder remembers the status code a handler wrote, so the
// middleware can label the metric with it.
type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.code = code
	r.ResponseWriter.WriteHeader(code)
}

// instrument wraps a handler and records count and latency. The path label
// is a fixed string passed in by the caller, never the raw URL, so a
// scanner hitting random URLs cannot create unlimited metric series.
func instrument(path string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(rec, r)
		requests.WithLabelValues(path, strconv.Itoa(rec.code)).Inc()
		duration.WithLabelValues(path).Observe(time.Since(start).Seconds())
	})
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func (c config) hello(w http.ResponseWriter, r *http.Request) {
	if c.latency > 0 {
		// +/- 50% jitter so the histogram looks like real traffic.
		time.Sleep(time.Duration(float64(c.latency) * (0.5 + rand.Float64())))
	}
	if rand.Float64() < c.errorRate {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "simulated failure"})
		return
	}
	host, _ := os.Hostname()
	writeJSON(w, http.StatusOK, map[string]string{
		"message": "hello from artifact-pipeline",
		"version": version,
		"pod":     host,
	})
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := loadConfig()
	info.WithLabelValues(version).Set(1)

	mux := http.NewServeMux()
	mux.Handle("GET /{$}", instrument("/", http.HandlerFunc(cfg.hello)))
	mux.Handle("GET /metrics", promhttp.Handler())
	// /healthz is not instrumented: probe traffic would dilute the error
	// rate. It also always answers 200, so a readiness probe passes even
	// when ERROR_RATE is high. Only real traffic metrics can see that bug.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{
		Addr:              ":" + cfg.port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Stop on SIGTERM (what Kubernetes sends) or Ctrl-C, then finish
	// in-flight requests before exiting.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	go func() {
		log.Info("listening", "port", cfg.port, "version", version,
			"error_rate", cfg.errorRate, "latency", cfg.latency.String())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server failed", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown failed", "err", err)
	}
}
