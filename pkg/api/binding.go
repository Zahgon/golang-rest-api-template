package api

import (
	"encoding/json"

	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"
)

// RequestValidator implements echo.Validator on top of go-playground/validator.
// Echo's binder binds without validating, so validation is wired here. The tag
// name is `binding`, matching the struct tags the models already carry.
type RequestValidator struct {
	validate *validator.Validate
}

// NewRequestValidator returns the echo.Validator used for request payloads.
func NewRequestValidator() *RequestValidator {
	v := validator.New()
	v.SetTagName("binding")
	return &RequestValidator{validate: v}
}

// Validate implements echo.Validator.
func (rv *RequestValidator) Validate(i any) error {
	return rv.validate.Struct(i)
}

// bindJSON decodes the JSON request body into i and validates it against the
// `binding` struct tags.
//
// The body is decoded as JSON regardless of Content-Type. Echo's own binder
// negotiates on that header and would reject a JSON payload sent without it
// (415) or bind nothing at all when a client labels it as form data, so
// decoding directly keeps every caller working as it did before. An empty body
// yields io.EOF, which callers that allow one (LogoutHandler) check for.
func bindJSON(c echo.Context, i any) error {
	if err := json.NewDecoder(c.Request().Body).Decode(i); err != nil {
		return err
	}
	return c.Validate(i)
}
