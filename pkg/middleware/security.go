package middleware

import (
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// headerXDownloadOptions is the IE "no open" header. Echo's Secure middleware
// has no IENoOpen option, so it is set alongside the Secure headers.
const headerXDownloadOptions = "X-Download-Options"

func Security() echo.MiddlewareFunc {
	secure := middleware.SecureWithConfig(middleware.SecureConfig{
		//AllowedHosts:          []string{"example.com", "ssl.example.com"},
		//SSLRedirect:           true,
		//SSLHost:               "ssl.example.com",
		HSTSMaxAge:            315360000,
		HSTSExcludeSubdomains: false,
		XFrameOptions:         "DENY",
		ContentTypeNosniff:    "nosniff",
		XSSProtection:         "1; mode=block",
		ContentSecurityPolicy: "default-src 'self'",
		ReferrerPolicy:        "strict-origin-when-cross-origin",
	})
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		secured := secure(next)
		return func(c echo.Context) error {
			c.Response().Header().Set(headerXDownloadOptions, "noopen")
			return secured(c)
		}
	}
}
