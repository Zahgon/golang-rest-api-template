package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"golang-rest-api-template/pkg/cache"

	"github.com/go-redis/redis/v8"
	"github.com/labstack/echo/v4"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/readpref"
	"gorm.io/gorm"
)

// registerProbeRoutes mounts Kubernetes-style liveness and readiness endpoints.
// They are registered on the Echo instance itself, outside the group that
// carries request logging, rate limiting, and the /api/v1 routes, so
// orchestrators can probe without API keys or JWTs.
func registerProbeRoutes(e *echo.Echo, db *gorm.DB, redisClient cache.Cache, mongoCol *mongo.Collection) {
	e.GET("/livez", livez)
	e.GET("/readyz", readyzHandler(db, redisClient, mongoCol))
}

func livez(c echo.Context) error {
	return c.NoContent(http.StatusOK)
}

func readyzHandler(db *gorm.DB, redisClient cache.Cache, mongoCol *mongo.Collection) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx, cancel := context.WithTimeout(c.Request().Context(), 2*time.Second)
		defer cancel()

		if err := pingPostgres(ctx, db); err != nil {
			return c.String(http.StatusServiceUnavailable, fmt.Sprintf("postgres: %v", err))
		}
		if err := pingRedis(ctx, redisClient); err != nil {
			return c.String(http.StatusServiceUnavailable, fmt.Sprintf("redis: %v", err))
		}
		if err := pingMongo(ctx, mongoCol); err != nil {
			return c.String(http.StatusServiceUnavailable, fmt.Sprintf("mongo: %v", err))
		}
		return c.NoContent(http.StatusOK)
	}
}

func pingPostgres(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return errors.New("database not configured")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

// redisPinger matches *redis.Client without importing the concrete type in Cache.
type redisPinger interface {
	Ping(ctx context.Context) *redis.StatusCmd
}

func pingRedis(ctx context.Context, c cache.Cache) error {
	if c == nil {
		return errors.New("redis not configured")
	}
	p, ok := c.(redisPinger)
	if !ok {
		return errors.New("redis client type does not support ping")
	}
	if err := p.Ping(ctx).Err(); err != nil {
		return err
	}
	return nil
}

func pingMongo(ctx context.Context, col *mongo.Collection) error {
	if col == nil {
		return errors.New("mongo collection not configured")
	}
	client := col.Database().Client()
	if client == nil {
		return fmt.Errorf("mongo client nil")
	}
	return client.Ping(ctx, readpref.Primary())
}
