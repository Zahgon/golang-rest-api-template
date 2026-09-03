package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestMaxRequestBodyAllowsSmallPayload(t *testing.T) {
	r := echo.New()
	const limit = 1024
	r.POST("/p", func(c echo.Context) error {
		if _, err := io.ReadAll(c.Request().Body); err != nil {
			return c.NoContent(http.StatusInternalServerError)
		}
		return c.NoContent(http.StatusOK)
	}, MaxRequestBody(limit))
	req := httptest.NewRequest(http.MethodPost, "/p", strings.NewReader(`{"x":1}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%q", rec.Code, rec.Body.String())
	}
}

func TestMaxRequestBodyRejectsOversizedContentLength(t *testing.T) {
	r := echo.New()
	const limit = 100
	r.POST("/p", func(c echo.Context) error {
		return c.NoContent(http.StatusOK)
	}, MaxRequestBody(limit))
	body := bytes.Repeat([]byte("a"), limit+1)
	req := httptest.NewRequest(http.MethodPost, "/p", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = int64(len(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d", rec.Code)
	}
}

func TestMaxRequestBodyMaxBytesReaderWithoutContentLength(t *testing.T) {
	r := echo.New()
	const limit = 64
	r.POST("/p", func(c echo.Context) error {
		if _, err := io.Copy(io.Discard, c.Request().Body); err != nil {
			return c.NoContent(http.StatusRequestEntityTooLarge)
		}
		return c.NoContent(http.StatusOK)
	}, MaxRequestBody(limit))
	// ContentLength -1: reader still capped
	req := httptest.NewRequest(http.MethodPost, "/p", bytes.NewReader(bytes.Repeat([]byte("b"), limit+10)))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = -1
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d body=%q", rec.Code, rec.Body.String())
	}
}
