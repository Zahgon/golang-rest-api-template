package httperr

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func newTestContext(w http.ResponseWriter) echo.Context {
	return echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/", nil), w)
}

func TestWrite(t *testing.T) {
	w := httptest.NewRecorder()
	c := newTestContext(w)
	assert.NoError(t, Write(c, http.StatusTeapot, "short and stout"))
	assert.Equal(t, http.StatusTeapot, w.Code)
	var body ErrorBody
	assert.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "short and stout", body.Error)
}

func TestAbort(t *testing.T) {
	w := httptest.NewRecorder()
	c := newTestContext(w)
	assert.NoError(t, Abort(c, http.StatusForbidden, "nope"))
	assert.Equal(t, http.StatusForbidden, w.Code)
	// Echo has no abort flag; a committed response is what stops later writes.
	assert.True(t, c.Response().Committed)
	var body ErrorBody
	assert.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "nope", body.Error)
}
