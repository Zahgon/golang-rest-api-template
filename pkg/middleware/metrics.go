package middleware

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	// DefaultMetricsPath is the HTTP path where Prometheus scrapes metrics.
	DefaultMetricsPath = "/metrics"

	unmatchedRouteLabel = "unmatched"
)

// MetricsConfig configures the Prometheus HTTP metrics middleware and endpoint.
type MetricsConfig struct {
	// Enabled turns metrics collection and the scrape endpoint on or off.
	Enabled bool
	// Path is the scrape path (default /metrics).
	Path string
}

// Metrics holds Prometheus collectors and Echo helpers for HTTP observability.
type Metrics struct {
	cfg      MetricsConfig
	registry *prometheus.Registry
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	inFlight prometheus.Gauge

	notFoundOnce  sync.Once
	notFoundPaths map[string]bool
}

// MetricsConfigFromEnv loads MetricsConfig from environment variables.
//
//	METRICS_ENABLED — true/false (default true; false/0/off/none disables)
//	METRICS_PATH    — scrape path (default /metrics); must start with /
//
// Invalid values log a warning and fall back to defaults.
func MetricsConfigFromEnv() MetricsConfig {
	cfg := MetricsConfig{
		Enabled: true,
		Path:    DefaultMetricsPath,
	}

	if s := strings.TrimSpace(os.Getenv("METRICS_ENABLED")); s != "" {
		switch strings.ToLower(s) {
		case "0", "false", "no", "off", "none":
			cfg.Enabled = false
		case "1", "true", "yes", "on":
			cfg.Enabled = true
		default:
			log.Printf("middleware: invalid METRICS_ENABLED=%q, using default true", s)
		}
	}

	if s := strings.TrimSpace(os.Getenv("METRICS_PATH")); s != "" {
		if !strings.HasPrefix(s, "/") || strings.Contains(s, " ") {
			log.Printf("middleware: invalid METRICS_PATH=%q, using default %s", s, DefaultMetricsPath)
		} else {
			cfg.Path = s
		}
	}

	return cfg
}

// NewMetrics creates HTTP metrics collectors on a dedicated registry.
// When cfg.Enabled is false, Middleware and Mount are no-ops.
func NewMetrics(cfg MetricsConfig) *Metrics {
	if cfg.Path == "" {
		cfg.Path = DefaultMetricsPath
	}

	m := &Metrics{cfg: cfg}
	if !cfg.Enabled {
		return m
	}

	reg := prometheus.NewRegistry()
	requests := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests processed.",
		},
		[]string{"method", "path", "status"},
	)
	duration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request duration in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)
	inFlight := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "http_requests_in_flight",
			Help: "Number of HTTP requests currently being processed.",
		},
	)

	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		requests,
		duration,
		inFlight,
	)

	m.registry = reg
	m.requests = requests
	m.duration = duration
	m.inFlight = inFlight
	return m
}

// MetricsFromEnv builds Metrics using MetricsConfigFromEnv.
func MetricsFromEnv() *Metrics {
	return NewMetrics(MetricsConfigFromEnv())
}

// Mount registers the Prometheus scrape endpoint on e when metrics are enabled.
// Register it on the Echo instance itself rather than the middleware-carrying
// group (same pattern as probes) so scrapes skip rate limiting and auth.
func (m *Metrics) Mount(e *echo.Echo) {
	if m == nil || !m.cfg.Enabled || e == nil {
		return
	}
	e.GET(m.cfg.Path, m.Handler())
}

// Handler serves Prometheus metrics in the text exposition format.
func (m *Metrics) Handler() echo.HandlerFunc {
	if m == nil || !m.cfg.Enabled || m.registry == nil {
		return func(c echo.Context) error {
			return c.NoContent(http.StatusNotFound)
		}
	}
	h := promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
	return func(c echo.Context) error {
		h.ServeHTTP(c.Response(), c.Request())
		return nil
	}
}

// Middleware records request count, latency, and in-flight gauges.
// It skips the scrape path and Swagger UI to avoid noisy or recursive series.
func (m *Metrics) Middleware() echo.MiddlewareFunc {
	if m == nil || !m.cfg.Enabled {
		return func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error { return next(c) }
		}
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if m.shouldSkip(c) {
				return next(c)
			}

			m.inFlight.Inc()
			defer m.inFlight.Dec()

			start := time.Now()
			err := next(c)
			if err != nil {
				// Commit the error response now so the recorded status is final.
				c.Error(err)
			}

			route := m.routeLabel(c)
			status := strconv.Itoa(c.Response().Status)
			method := c.Request().Method

			m.requests.WithLabelValues(method, route, status).Inc()
			m.duration.WithLabelValues(method, route).Observe(time.Since(start).Seconds())

			return err
		}
	}
}

// routeLabel returns the "path" label for the matched route. Echo registers
// catch-all "route not found" entries for every group that carries middleware
// (so that middleware still runs for unmatched paths); those report wildcard
// patterns like /* or /api/v1/*, which are collapsed into a single unmatched
// label rather than being reported as if they were real routes.
func (m *Metrics) routeLabel(c echo.Context) string {
	route := c.Path()
	if route == "" {
		return unmatchedRouteLabel
	}
	if m.notFoundRouteSet(c)[route] {
		return unmatchedRouteLabel
	}
	return route
}

// notFoundRouteSet returns the paths Echo registered as route-not-found
// handlers, resolved once from the first request (all routes are registered
// during router construction, before the server starts serving).
func (m *Metrics) notFoundRouteSet(c echo.Context) map[string]bool {
	m.notFoundOnce.Do(func() {
		paths := make(map[string]bool)
		if e := c.Echo(); e != nil {
			for _, r := range e.Routes() {
				if r.Method == echo.RouteNotFound {
					paths[r.Path] = true
				}
			}
		}
		m.notFoundPaths = paths
	})
	return m.notFoundPaths
}

func (m *Metrics) shouldSkip(c echo.Context) bool {
	path := c.Request().URL.Path
	if path == m.cfg.Path {
		return true
	}
	if strings.HasPrefix(path, "/swagger") {
		return true
	}
	return false
}

// Enabled reports whether metrics collection is active.
func (m *Metrics) Enabled() bool {
	return m != nil && m.cfg.Enabled
}

// Path returns the configured scrape path.
func (m *Metrics) Path() string {
	if m == nil || m.cfg.Path == "" {
		return DefaultMetricsPath
	}
	return m.cfg.Path
}
