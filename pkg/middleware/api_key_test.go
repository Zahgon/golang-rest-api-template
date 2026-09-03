package middleware

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func testRouterAPIKeyOnly(t *testing.T) *echo.Echo {
	t.Helper()
	r := echo.New()
	r.GET("/k", func(c echo.Context) error {
		return c.NoContent(http.StatusOK)
	}, APIKeyAuth())
	return r
}

func TestSetAPISecretKeyRejectsShortSecret(t *testing.T) {
	want := apiSecretCopy()
	err := SetAPISecretKey(bytes.Repeat([]byte("p"), MinAPISecretKeyBytes-1))
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrAPISecretKeyTooShort)
	assert.Equal(t, want, apiSecretCopy())
}

func TestAPIKeyAuthValidKey(t *testing.T) {
	r := testRouterAPIKeyOnly(t)
	req := httptest.NewRequest(http.MethodGet, "/k", nil)
	req.Header.Set("X-API-Key", strings.Repeat("x", MinAPISecretKeyBytes))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestAPIKeyAuthWrongKey(t *testing.T) {
	r := testRouterAPIKeyOnly(t)
	req := httptest.NewRequest(http.MethodGet, "/k", nil)
	req.Header.Set("X-API-Key", strings.Repeat("y", MinAPISecretKeyBytes))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	var body map[string]any
	assert.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "Unauthorized", body["error"])
}

func TestAPIKeyAuthWrongLengthRejected(t *testing.T) {
	r := testRouterAPIKeyOnly(t)
	req := httptest.NewRequest(http.MethodGet, "/k", nil)
	req.Header.Set("X-API-Key", strings.Repeat("x", MinAPISecretKeyBytes-1))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAPIKeyAuthMissingHeader(t *testing.T) {
	r := testRouterAPIKeyOnly(t)
	req := httptest.NewRequest(http.MethodGet, "/k", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAPIKeyAuthSecretNotConfiguredReturns503(t *testing.T) {
	prev := apiSecretCopy()
	ClearAPISecretKeyForTesting()
	t.Cleanup(func() {
		assert.NoError(t, SetAPISecretKey(prev))
	})
	r := testRouterAPIKeyOnly(t)
	req := httptest.NewRequest(http.MethodGet, "/k", nil)
	req.Header.Set("X-API-Key", strings.Repeat("x", MinAPISecretKeyBytes))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestAPIKeyAuthConcurrentValidRequests(t *testing.T) {
	r := testRouterAPIKeyOnly(t)
	const workers = 32
	key := strings.Repeat("x", MinAPISecretKeyBytes)
	var wg sync.WaitGroup
	errs := make(chan string, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/k", nil)
			req.Header.Set("X-API-Key", key)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				errs <- w.Body.String()
			}
		}()
	}
	wg.Wait()
	close(errs)
	for msg := range errs {
		if msg != "" {
			t.Fatalf("unexpected failure body=%q", msg)
		}
	}
}
