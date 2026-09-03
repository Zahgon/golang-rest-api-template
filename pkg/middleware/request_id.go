package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/labstack/echo/v4"
)

const RequestIDHeader = "X-Request-Id"

const requestIDStoreKey = "request_id"

type requestIDContextKey struct{}

func newRequestID() string {
	var b [16]byte
	_, err := rand.Read(b[:])
	if err != nil {
		return ""
	}

	// Set version (4) and variant (10)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	buf := make([]byte, 36)
	hex.Encode(buf[0:8], b[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], b[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], b[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], b[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], b[10:16])
	return string(buf)
}

// RequestID ensures every request has a stable request id.
func RequestID() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			requestID := strings.TrimSpace(c.Request().Header.Get(RequestIDHeader))
			if requestID == "" {
				requestID = newRequestID()
			}

			if requestID == "" {
				requestID = "unknown"
			}

			c.Set(requestIDStoreKey, requestID)
			c.Response().Header().Set(RequestIDHeader, requestID)
			c.SetRequest(c.Request().WithContext(context.WithValue(c.Request().Context(), requestIDContextKey{}, requestID)))

			return next(c)
		}
	}
}

// GetRequestID returns the request id stored in echo context.
func GetRequestID(c echo.Context) string {
	if c == nil {
		return ""
	}
	if requestID, ok := c.Get(requestIDStoreKey).(string); ok {
		return requestID
	}
	return ""
}

// GetRequestIDFromContext reads the request id from context.
func GetRequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if value := ctx.Value(requestIDContextKey{}); value != nil {
		if requestID, ok := value.(string); ok {
			return requestID
		}
	}
	return ""
}
