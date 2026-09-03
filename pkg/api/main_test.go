package api

import (
	"bytes"

	"golang-rest-api-template/pkg/auth"
	"golang-rest-api-template/pkg/middleware"

	"github.com/labstack/echo/v4"
)

// testAPISecretKey is the X-API-Key value expected by middleware in tests (32 bytes).
const testAPISecretKey = "01234567890123456789012345678901"

func init() {
	// Runs for the full package test binary and whenever this file is linked (e.g. with
	// `go test main_test.go other_test.go`); configures JWT + API key before any test.
	if err := auth.SetJWTSigningKey(bytes.Repeat([]byte("t"), auth.MinJWTSecretKeyBytes)); err != nil {
		panic(err)
	}
	if err := middleware.SetAPISecretKey([]byte(testAPISecretKey)); err != nil {
		panic(err)
	}
}

// newTestEcho returns an Echo instance wired the way NewRouter wires it for
// request payloads, so handlers under test bind and validate identically.
func newTestEcho() *echo.Echo {
	e := echo.New()
	e.Validator = NewRequestValidator()
	return e
}
