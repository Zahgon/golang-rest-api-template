package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func TestRequestContextTimeoutNoOp(t *testing.T) {
	r := echo.New()
	r.Use(RequestContextTimeout(0))
	r.GET("/ok", func(c echo.Context) error {
		assert.NoError(t, c.Request().Context().Err())
		return c.NoContent(http.StatusNoContent)
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ok", nil))
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestRequestContextTimeoutDurationFromEnv(t *testing.T) {
	t.Run("empty uses default", func(t *testing.T) {
		t.Setenv("REQUEST_CONTEXT_TIMEOUT", "")
		assert.Equal(t, defaultRequestContextTimeout, requestContextTimeoutDurationFromEnv())
	})
	t.Run("off disables", func(t *testing.T) {
		t.Setenv("REQUEST_CONTEXT_TIMEOUT", "off")
		assert.Equal(t, time.Duration(0), requestContextTimeoutDurationFromEnv())
	})
	t.Run("parse duration", func(t *testing.T) {
		t.Setenv("REQUEST_CONTEXT_TIMEOUT", "2m")
		assert.Equal(t, 2*time.Minute, requestContextTimeoutDurationFromEnv())
	})
}

func TestRequestContextTimeoutFiresBeforeSlowWork(t *testing.T) {
	r := echo.New()
	r.Use(RequestContextTimeout(40 * time.Millisecond))
	r.GET("/slow", func(c echo.Context) error {
		ctx := c.Request().Context()
		select {
		case <-time.After(500 * time.Millisecond):
			return c.NoContent(http.StatusOK)
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return c.NoContent(http.StatusGatewayTimeout)
			}
			return c.NoContent(http.StatusInternalServerError)
		}
	})
	rec := httptest.NewRecorder()
	start := time.Now()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/slow", nil))
	assert.Less(t, time.Since(start), 200*time.Millisecond, "handler should stop when context times out")
	assert.Equal(t, http.StatusGatewayTimeout, rec.Code)
}
