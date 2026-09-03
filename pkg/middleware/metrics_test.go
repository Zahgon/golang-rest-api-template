package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricsConfigFromEnv(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		t.Setenv("METRICS_ENABLED", "")
		t.Setenv("METRICS_PATH", "")
		cfg := MetricsConfigFromEnv()
		assert.True(t, cfg.Enabled)
		assert.Equal(t, DefaultMetricsPath, cfg.Path)
	})
	t.Run("disabled", func(t *testing.T) {
		t.Setenv("METRICS_ENABLED", "off")
		cfg := MetricsConfigFromEnv()
		assert.False(t, cfg.Enabled)
	})
	t.Run("custom path", func(t *testing.T) {
		t.Setenv("METRICS_PATH", "/internal/metrics")
		cfg := MetricsConfigFromEnv()
		assert.Equal(t, "/internal/metrics", cfg.Path)
	})
	t.Run("invalid path falls back", func(t *testing.T) {
		t.Setenv("METRICS_PATH", "metrics")
		cfg := MetricsConfigFromEnv()
		assert.Equal(t, DefaultMetricsPath, cfg.Path)
	})
}

func TestMetricsEndpointAndInstrumentation(t *testing.T) {
	m := NewMetrics(MetricsConfig{Enabled: true, Path: DefaultMetricsPath})
	r := echo.New()
	m.Mount(r)
	r.Use(m.Middleware())
	r.GET("/api/v1/books", func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})
	r.GET("/api/v1/books/:id", func(c echo.Context) error {
		return c.NoContent(http.StatusNotFound)
	})

	// Instrumented request
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/books", nil))
	assert.Equal(t, http.StatusOK, rec.Code)

	// Unmatched route
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)

	// Scrape
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "http_requests_total")
	assert.Contains(t, body, "http_request_duration_seconds")
	assert.Contains(t, body, "http_requests_in_flight")
	assert.Contains(t, body, `path="/api/v1/books"`)
	assert.Contains(t, body, `path="unmatched"`)
	// Scrape itself must not appear as an instrumented path series.
	assert.NotContains(t, body, `path="/metrics"`)
}

func TestMetricsDisabled(t *testing.T) {
	m := NewMetrics(MetricsConfig{Enabled: false})
	r := echo.New()
	m.Mount(r)
	r.Use(m.Middleware())
	r.GET("/ok", func(c echo.Context) error { return c.NoContent(http.StatusNoContent) })

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ok", nil))
	assert.Equal(t, http.StatusNoContent, rec.Code)

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestMetricsSkipsSwagger(t *testing.T) {
	m := NewMetrics(MetricsConfig{Enabled: true})
	r := echo.New()
	m.Mount(r)
	r.Use(m.Middleware())
	r.GET("/swagger/*", func(c echo.Context) error {
		return c.String(http.StatusOK, "docs")
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/swagger/index.html", nil))
	assert.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), `path="/swagger/*"`)
}

func TestMetricsCounterValues(t *testing.T) {
	m := NewMetrics(MetricsConfig{Enabled: true})
	r := echo.New()
	r.Use(m.Middleware())
	r.GET("/ping", func(c echo.Context) error { return c.NoContent(http.StatusOK) })

	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ping", nil))
		assert.Equal(t, http.StatusOK, rec.Code)
	}

	metric := &dto.Metric{}
	err := m.requests.WithLabelValues(http.MethodGet, "/ping", "200").Write(metric)
	require.NoError(t, err)
	assert.Equal(t, float64(3), metric.GetCounter().GetValue())
}

func TestMetricsConcurrent(t *testing.T) {
	m := NewMetrics(MetricsConfig{Enabled: true})
	r := echo.New()
	r.Use(m.Middleware())
	r.GET("/ping", func(c echo.Context) error { return c.NoContent(http.StatusOK) })

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ping", nil))
			assert.Equal(t, http.StatusOK, rec.Code)
		}()
	}
	wg.Wait()

	metric := &dto.Metric{}
	err := m.requests.WithLabelValues(http.MethodGet, "/ping", "200").Write(metric)
	require.NoError(t, err)
	assert.Equal(t, float64(n), metric.GetCounter().GetValue())
}

func TestMetricsGathererIncludesGoCollectors(t *testing.T) {
	m := NewMetrics(MetricsConfig{Enabled: true})
	// CounterVec/HistogramVec series appear only after the first observation.
	m.requests.WithLabelValues(http.MethodGet, "/ping", "200").Inc()
	families, err := m.registry.Gather()
	require.NoError(t, err)
	var names []string
	for _, f := range families {
		names = append(names, f.GetName())
	}
	joined := strings.Join(names, ",")
	assert.Contains(t, joined, "go_")
	assert.Contains(t, joined, "process_")
	assert.Contains(t, joined, "http_requests_total")
}

// Ensure Metrics implements expected registration without touching the global registry.
func TestMetricsUsesDedicatedRegistry(t *testing.T) {
	m1 := NewMetrics(MetricsConfig{Enabled: true})
	m2 := NewMetrics(MetricsConfig{Enabled: true})
	require.NotNil(t, m1.registry)
	require.NotNil(t, m2.registry)
	assert.NotSame(t, m1.registry, m2.registry)

	// Global default registerer should not own our collectors.
	assert.NotEqual(t, prometheus.DefaultRegisterer, m1.registry)
}

// Echo registers catch-all route-not-found entries for groups carrying
// middleware, so unmatched paths report wildcard patterns instead of "". They
// must still be counted under the single unmatched label, and real wildcard
// routes must keep their own label.
func TestMetricsLabelsGroupCatchAllAsUnmatched(t *testing.T) {
	m := NewMetrics(MetricsConfig{Enabled: true})
	r := echo.New()
	g := r.Group("")
	g.Use(m.Middleware())
	g.GET("/api/v1/books", func(c echo.Context) error { return c.NoContent(http.StatusOK) })
	g.GET("/files/*", func(c echo.Context) error { return c.NoContent(http.StatusOK) })

	for _, path := range []string{"/api/v1/books", "/files/a.txt", "/nope", "/api/v1/nope"} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	}

	metric := &dto.Metric{}
	require.NoError(t, m.requests.WithLabelValues(http.MethodGet, unmatchedRouteLabel, "404").Write(metric))
	assert.Equal(t, float64(2), metric.GetCounter().GetValue(), "both unmatched paths share the unmatched label")

	// A genuinely registered wildcard route keeps its own label.
	metric = &dto.Metric{}
	require.NoError(t, m.requests.WithLabelValues(http.MethodGet, "/files/*", "200").Write(metric))
	assert.Equal(t, float64(1), metric.GetCounter().GetValue())
}
