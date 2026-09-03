package middleware

import (
	"context"
	"log"
	"os"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

const defaultRequestContextTimeout = 60 * time.Second

// RequestContextTimeout returns middleware that attaches a derived
// context.WithTimeout to the request, so handlers and services using
// c.Request().Context() inherit a deadline (#127). If d <= 0, the middleware is
// a no-op.
func RequestContextTimeout(d time.Duration) echo.MiddlewareFunc {
	if d <= 0 {
		return func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error { return next(c) }
		}
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx, cancel := context.WithTimeout(c.Request().Context(), d)
			defer cancel()
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	}
}

// RequestContextTimeoutFromEnv is like RequestContextTimeout with a duration
// from REQUEST_CONTEXT_TIMEOUT (Go duration syntax). Empty uses 60s. "0",
// "off", or "none" disables the middleware. Invalid values log a warning and
// fall back to the default.
func RequestContextTimeoutFromEnv() echo.MiddlewareFunc {
	return RequestContextTimeout(requestContextTimeoutDurationFromEnv())
}

func requestContextTimeoutDurationFromEnv() time.Duration {
	s := strings.TrimSpace(os.Getenv("REQUEST_CONTEXT_TIMEOUT"))
	if s == "" {
		return defaultRequestContextTimeout
	}
	if strings.EqualFold(s, "0") || strings.EqualFold(s, "off") || strings.EqualFold(s, "none") {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		log.Printf("middleware: invalid REQUEST_CONTEXT_TIMEOUT=%q, using default %v", s, defaultRequestContextTimeout)
		return defaultRequestContextTimeout
	}
	return d
}
