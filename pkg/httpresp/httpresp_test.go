package httpresp

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

func TestOKStringEnvelope(t *testing.T) {
	w := httptest.NewRecorder()
	c := newTestContext(w)
	assert.NoError(t, OK(c, "ok"))
	assert.Equal(t, http.StatusOK, w.Code)
	var out struct {
		Data string `json:"data"`
	}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.Equal(t, "ok", out.Data)
}

func TestCreatedStructEnvelope(t *testing.T) {
	w := httptest.NewRecorder()
	c := newTestContext(w)
	type item struct {
		ID int `json:"id"`
	}
	assert.NoError(t, Created(c, item{ID: 42}))
	assert.Equal(t, http.StatusCreated, w.Code)
	var out struct {
		Data struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.Equal(t, 42, out.Data.ID)
}
