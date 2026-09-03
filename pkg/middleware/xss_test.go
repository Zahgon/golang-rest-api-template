package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

const dirty = `<script>alert(1)</script>hello`
const clean = "hello"

// run sends req through the Xss middleware and returns what the handler saw.
func run(t *testing.T, req *http.Request, capture func(c echo.Context) string) string {
	t.Helper()
	r := echo.New()
	var seen string
	h := func(c echo.Context) error {
		seen = capture(c)
		return c.NoContent(http.StatusOK)
	}
	r.Add(req.Method, "/p", h, Xss())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	return seen
}

func TestXssSanitizesGETQueryString(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/p?q="+"%3Cscript%3Ealert(1)%3C/script%3Ehello", nil)
	got := run(t, req, func(c echo.Context) string { return c.QueryParam("q") })
	if got != clean {
		t.Fatalf("query not sanitized: want %q, got %q", clean, got)
	}
}

func TestXssSkipsPasswordFieldOnGET(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/p?password="+"%3Cb%3Ep%40ss%3C/b%3E", nil)
	got := run(t, req, func(c echo.Context) string { return c.QueryParam("password") })
	if got != "<b>p@ss</b>" {
		t.Fatalf("password must pass through unfiltered, got %q", got)
	}
}

func TestXssSanitizesFormEncodedBody(t *testing.T) {
	body := "name=" + "%3Cscript%3Ealert(1)%3C/script%3Ehello" + "&password=%3Cb%3Ex%3C/b%3E"
	req := httptest.NewRequest(http.MethodPost, "/p", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Content-Length", strconv.Itoa(len(body)))

	r := echo.New()
	var name, pass string
	r.POST("/p", func(c echo.Context) error {
		name = c.FormValue("name")
		pass = c.FormValue("password")
		return c.NoContent(http.StatusOK)
	}, Xss())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if name != clean {
		t.Fatalf("form field not sanitized: want %q, got %q", clean, name)
	}
	if pass != "<b>x</b>" {
		t.Fatalf("password must pass through unfiltered, got %q", pass)
	}
}

func TestXssSanitizesJSONBody(t *testing.T) {
	payload := `{"name":"<script>alert(1)</script>hello","password":"<b>x</b>","n":7}`
	req := httptest.NewRequest(http.MethodPost, "/p", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Length", strconv.Itoa(len(payload)))

	raw := run(t, req, func(c echo.Context) string {
		b, _ := io.ReadAll(c.Request().Body)
		return string(b)
	})

	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("handler received invalid JSON %q: %v", raw, err)
	}
	if out["name"] != clean {
		t.Fatalf("json field not sanitized: want %q, got %v", clean, out["name"])
	}
	if out["password"] != "<b>x</b>" {
		t.Fatalf("password must pass through unfiltered, got %v", out["password"])
	}
}

func TestXssSanitizesMultipartFormData(t *testing.T) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("name", dirty)
	_ = w.WriteField("password", "<b>x</b>")
	_ = w.Close()

	req := httptest.NewRequest(http.MethodPost, "/p", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Content-Length", strconv.Itoa(buf.Len()))

	r := echo.New()
	var name, pass string
	r.POST("/p", func(c echo.Context) error {
		name = c.FormValue("name")
		pass = c.FormValue("password")
		return c.NoContent(http.StatusOK)
	}, Xss())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	if name != clean {
		t.Fatalf("multipart field not sanitized: want %q, got %q", clean, name)
	}
	if pass != "<b>x</b>" {
		t.Fatalf("password must pass through unfiltered, got %q", pass)
	}
}

func TestXssLeavesDeleteRequestsAlone(t *testing.T) {
	req := httptest.NewRequest(http.MethodDelete, "/p?q="+"%3Cb%3Ez%3C/b%3E", nil)
	got := run(t, req, func(c echo.Context) string { return c.QueryParam("q") })
	if got != "<b>z</b>" {
		t.Fatalf("DELETE must be untouched, got %q", got)
	}
}
