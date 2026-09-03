package api

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"golang-rest-api-template/pkg/auth"
	"golang-rest-api-template/pkg/cache"
	"golang-rest-api-template/pkg/middleware"
	"golang-rest-api-template/pkg/repository"

	docs "golang-rest-api-template/docs"

	"github.com/labstack/echo/v4"
	echomw "github.com/labstack/echo/v4/middleware"
	echoSwagger "github.com/swaggo/echo-swagger"
	"go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// releaseMode reports whether the application runs in release mode
// (APP_MODE=release), which enables the Security and XSS middleware. Echo has
// no framework-level mode, so the setting is read straight from the
// environment.
func releaseMode() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("APP_MODE")), "release")
}

// NewRouter builds the Echo instance with middleware, Swagger, and API routes.
func NewRouter(logger *zap.Logger, mongoCollection *mongo.Collection, db *gorm.DB, redisClient cache.Cache) *echo.Echo {
	denylist := auth.NewTokenDenylistFromEnv(redisClient)
	jwtAuth := middleware.JWTAuth(denylist)

	books := NewBookHandler(repository.NewGormBookStore(db), redisClient)
	users := NewUserHandler(
		repository.NewGormUserStore(db),
		repository.NewGormRefreshTokenStore(db),
		denylist,
	)

	r := echo.New()
	r.Validator = NewRequestValidator()
	r.Use(echomw.Logger(), echomw.Recover())
	if err := configureTrustedProxies(r); err != nil {
		panic("api: trusted proxies: " + err.Error())
	}
	registerProbeRoutes(r, db, redisClient, mongoCollection)

	// Prometheus scrape endpoint (mounted on the Echo instance rather than the
	// middleware-carrying group below, so scrapers skip rate limits).
	metrics := middleware.MetricsFromEnv()
	metrics.Mount(r)

	// Echo applies e.Use middleware to every request regardless of route
	// registration order, so the request pipeline lives on a root group that
	// the probe and metrics routes above are not part of.
	root := r.Group("")
	root.Use(metrics.Middleware())
	root.Use(middleware.MaxRequestBody(maxRequestBodyBytesFromEnv()))
	root.Use(middleware.RequestID())
	root.Use(middleware.Tracing())

	//root.Use(echomw.Logger())
	root.Use(middleware.Logger(logger, mongoCollection))
	if releaseMode() {
		root.Use(middleware.Security())
		root.Use(middleware.Xss())
	}
	root.Use(middleware.Cors())
	// Per-client fixed-window limiter (Redis by default; see RATE_LIMIT_* env).
	root.Use(middleware.RateLimiterFromEnv(redisClient))

	docs.SwaggerInfo.BasePath = "/api/v1"
	v1 := root.Group("/api/v1")
	v1.Use(middleware.RequestContextTimeoutFromEnv())
	{
		v1.GET("/", books.Healthcheck)
		v1.GET("/books", books.FindBooks, middleware.APIKeyAuth())
		v1.POST("/books", books.CreateBook, middleware.APIKeyAuth(), jwtAuth)
		v1.GET("/books/:id", books.FindBook, middleware.APIKeyAuth())
		v1.PUT("/books/:id", books.UpdateBook, middleware.APIKeyAuth(), jwtAuth)
		v1.PATCH("/books/:id", books.PatchBook, middleware.APIKeyAuth(), jwtAuth)
		v1.DELETE("/books/:id", books.DeleteBook, middleware.APIKeyAuth(), jwtAuth)

		v1.POST("/login", users.LoginHandler, middleware.APIKeyAuth())
		v1.POST("/register", users.RegisterHandler, middleware.APIKeyAuth())
		v1.POST("/refresh", users.RefreshHandler, middleware.APIKeyAuth())
		v1.POST("/logout", users.LogoutHandler, middleware.APIKeyAuth(), jwtAuth)

		admin := v1.Group("/admin", middleware.APIKeyAuth(), jwtAuth, middleware.RequireRole(auth.RoleAdmin))
		{
			admin.GET("/me", users.AdminMeHandler)
		}
	}
	root.GET("/swagger/*", echoSwagger.WrapHandler)

	return r
}

// configureTrustedProxies sets which upstreams may influence RealIP via
// X-Forwarded-For and related headers. If TRUSTED_PROXIES is unset or blank, no
// proxies are trusted, so RealIP reflects the direct TCP peer only. Otherwise
// the value is a comma-separated list of IPs or CIDRs whose XFF entries are
// followed.
func configureTrustedProxies(e *echo.Echo) error {
	raw := strings.TrimSpace(os.Getenv("TRUSTED_PROXIES"))
	if raw == "" {
		e.IPExtractor = echo.ExtractIPDirect()
		return nil
	}
	// Only the configured ranges are trusted; Echo's defaults (loopback,
	// link-local, private nets) are turned off to match an explicit list.
	options := []echo.TrustOption{
		echo.TrustLoopback(false),
		echo.TrustLinkLocal(false),
		echo.TrustPrivateNet(false),
	}
	for _, p := range strings.Split(raw, ",") {
		s := strings.TrimSpace(p)
		if s == "" {
			continue
		}
		ipNet, err := parseTrustedProxy(s)
		if err != nil {
			return err
		}
		options = append(options, echo.TrustIPRange(ipNet))
	}
	e.IPExtractor = echo.ExtractIPFromXFFHeader(options...)
	return nil
}

// parseTrustedProxy accepts either a CIDR block or a single IP address (which
// becomes a host-sized range).
func parseTrustedProxy(s string) (*net.IPNet, error) {
	if _, ipNet, err := net.ParseCIDR(s); err == nil {
		return ipNet, nil
	}
	ip := net.ParseIP(s)
	if ip == nil {
		return nil, fmt.Errorf("invalid trusted proxy %q: must be an IP address or CIDR block", s)
	}
	bits := 8 * net.IPv6len
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
		bits = 8 * net.IPv4len
	}
	return &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)}, nil
}

// maxRequestBodyBytesFromEnv returns REQUEST_MAX_BODY_BYTES or the middleware
// default (1 MiB). Invalid or non-positive values panic at process startup.
func maxRequestBodyBytesFromEnv() int64 {
	s := strings.TrimSpace(os.Getenv("REQUEST_MAX_BODY_BYTES"))
	if s == "" {
		return middleware.DefaultMaxRequestBodyBytes
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		panic("api: REQUEST_MAX_BODY_BYTES must be a positive integer (bytes): " + s)
	}
	return n
}
