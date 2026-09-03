package middleware

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// DefaultMaxRequestBodyBytes is used when MaxRequestBody is called with a
// non-positive limit or when the caller passes zero explicitly.
const DefaultMaxRequestBodyBytes int64 = 1 << 20 // 1 MiB

// MaxRequestBody returns middleware that caps how many bytes handlers may read
// from the request body for POST, PUT, and PATCH. Larger bodies yield HTTP 413
// (via http.MaxBytesReader) without buffering the entire payload in memory.
func MaxRequestBody(maxBytes int64) echo.MiddlewareFunc {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxRequestBodyBytes
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			switch c.Request().Method {
			case http.MethodPost, http.MethodPut, http.MethodPatch:
			default:
				return next(c)
			}
			if c.Request().ContentLength > maxBytes {
				return c.NoContent(http.StatusRequestEntityTooLarge)
			}
			c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, maxBytes)
			return next(c)
		}
	}
}
