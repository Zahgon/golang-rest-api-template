package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func TestRequestIDGenerated(t *testing.T) {
	r := echo.New()
	r.Use(RequestID())
	r.GET("/test", func(c echo.Context) error {
		id1 := GetRequestID(c)
		id2 := GetRequestID(c)
		ctxID := GetRequestIDFromContext(c.Request().Context())
		return c.JSON(http.StatusOK, map[string]string{"id1": id1, "id2": id2, "ctx": ctxID})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	requestID := w.Header().Get(RequestIDHeader)
	assert.NotEmpty(t, requestID)

	var body map[string]string
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, requestID, body["id1"])
	assert.Equal(t, requestID, body["id2"])
	assert.Equal(t, requestID, body["ctx"])
}

func TestRequestIDPreserved(t *testing.T) {
	r := echo.New()
	r.Use(RequestID())
	r.GET("/test", func(c echo.Context) error {
		requestID := GetRequestID(c)
		ctxID := GetRequestIDFromContext(c.Request().Context())
		return c.JSON(http.StatusOK, map[string]string{"id": requestID, "ctx": ctxID})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(RequestIDHeader, "req-123")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "req-123", w.Header().Get(RequestIDHeader))

	var body map[string]string
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "req-123", body["id"])
	assert.Equal(t, "req-123", body["ctx"])
}
