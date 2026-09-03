package middleware

import (
	"golang-rest-api-template/pkg/auth"
	"golang-rest-api-template/pkg/httperr"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
)

// ContextUserID is the Echo context key for the authenticated user's numeric ID
// (users.id), set by JWTAuth after successful verification.
const ContextUserID = "user_id"

// ContextRole is the Echo context key for the authenticated user's role claim,
// set by JWTAuth after successful verification.
const ContextRole = "role"

// ContextJTI is the Echo context key for the access token jti claim.
const ContextJTI = "jti"

// ContextAccessExp is the Echo context key for the access token expiry time.Time.
const ContextAccessExp = "access_exp"

// JWTAuth returns Echo middleware that requires a valid Bearer JWT signed
// with HMAC-SHA256 using the application's JWT secret. Other algorithms are
// rejected before signature verification. When denylist is non-nil, revoked
// JTIs are rejected; a nil denylist skips the check (same as NoopDenylist).
func JWTAuth(denylist auth.TokenDenylist) echo.MiddlewareFunc {
	if denylist == nil {
		denylist = auth.NoopDenylist{}
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			const BearerSchema = "Bearer "
			header := c.Request().Header.Get("Authorization")
			if header == "" {
				return httperr.Abort(c, http.StatusUnauthorized, "Missing Authorization Header")
			}

			if !strings.HasPrefix(header, BearerSchema) {
				return httperr.Abort(c, http.StatusUnauthorized, "Invalid Authorization Header")
			}

			signingKey := auth.JWTSigningKey()
			if len(signingKey) < auth.MinJWTSecretKeyBytes {
				return httperr.Abort(c, http.StatusServiceUnavailable, "JWT signing key not configured")
			}

			tokenStr := header[len(BearerSchema):]
			claims := &auth.Claims{}

			token, err := jwt.ParseWithClaims(tokenStr, claims, auth.JWTKeyFunc(signingKey))

			if err != nil {
				return httperr.Abort(c, http.StatusUnauthorized, "Invalid token")
			}

			if !token.Valid {
				return httperr.Abort(c, http.StatusUnauthorized, "Invalid token")
			}

			if claims.UserID == 0 {
				return httperr.Abort(c, http.StatusUnauthorized, "Invalid token")
			}

			if !auth.ValidRole(claims.Role) {
				return httperr.Abort(c, http.StatusUnauthorized, "Invalid token")
			}

			if claims.ID != "" {
				denied, err := denylist.IsDenied(c.Request().Context(), claims.ID)
				if err != nil {
					return httperr.Abort(c, http.StatusUnauthorized, "Invalid token")
				}
				if denied {
					return httperr.Abort(c, http.StatusUnauthorized, "Token revoked")
				}
			}

			if claims.IssuedAt != nil {
				revoked, err := denylist.IsUserRevoked(c.Request().Context(), claims.UserID, claims.IssuedAt.Time)
				if err != nil {
					return httperr.Abort(c, http.StatusUnauthorized, "Invalid token")
				}
				if revoked {
					return httperr.Abort(c, http.StatusUnauthorized, "Token revoked")
				}
			}

			c.Set("username", claims.Username)
			c.Set(ContextUserID, claims.UserID)
			c.Set(ContextRole, claims.Role)
			c.Set(ContextJTI, claims.ID)
			if claims.ExpiresAt != nil {
				c.Set(ContextAccessExp, claims.ExpiresAt.Time)
			}
			return next(c)
		}
	}
}
