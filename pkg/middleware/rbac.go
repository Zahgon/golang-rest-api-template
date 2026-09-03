package middleware

import (
	"net/http"

	"golang-rest-api-template/pkg/httperr"

	"github.com/labstack/echo/v4"
)

// RequireRole returns Echo middleware that allows the request only when the
// authenticated role (ContextRole, set by JWTAuth) matches one of allowed.
// It must run after JWTAuth. Missing or disallowed roles yield 403 forbidden.
func RequireRole(allowed ...string) echo.MiddlewareFunc {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, r := range allowed {
		if r == "" {
			continue
		}
		allowedSet[r] = struct{}{}
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			role, _ := c.Get(ContextRole).(string)
			if _, ok := allowedSet[role]; !ok {
				return httperr.Abort(c, http.StatusForbidden, "forbidden")
			}
			return next(c)
		}
	}
}
