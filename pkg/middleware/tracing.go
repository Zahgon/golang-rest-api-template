package middleware

import (
	"fmt"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	tracerName           = "golang-rest-api-template/pkg/middleware"
	requestIDSpanAttrKey = "http.request_id"
)

// Tracing returns Echo middleware that records a server span for each request.
// It must run after RequestID so the X-Request-Id value is available; the id is
// stored on the span as http.request_id for correlation with access logs.
//
// When the global TracerProvider is the SDK no-op (tracing disabled), spans are
// cheap no-ops and propagation still runs harmlessly.
func Tracing() echo.MiddlewareFunc {
	tracer := otel.Tracer(tracerName)
	propagator := otel.GetTextMapPropagator()

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := propagator.Extract(c.Request().Context(), propagation.HeaderCarrier(c.Request().Header))

			route := c.Path()
			spanName := route
			if spanName == "" {
				// Keep unmatched span names low-cardinality; raw path stays on url.path.
				spanName = c.Request().Method + " unmatched"
			}

			attrs := []attribute.KeyValue{
				semconv.HTTPRequestMethodKey.String(c.Request().Method),
				semconv.URLPath(c.Request().URL.Path),
				semconv.URLScheme(requestScheme(c)),
			}
			if host := c.Request().Host; host != "" {
				attrs = append(attrs, semconv.ServerAddress(host))
			}
			if requestID := GetRequestID(c); requestID != "" {
				attrs = append(attrs, attribute.String(requestIDSpanAttrKey, requestID))
			}
			if route != "" {
				attrs = append(attrs, semconv.HTTPRoute(route))
			}

			ctx, span := tracer.Start(ctx, spanName,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(attrs...),
			)
			defer span.End()

			c.SetRequest(c.Request().WithContext(ctx))
			err := next(c)
			if err != nil {
				// Commit the error response now so the recorded status is final.
				c.Error(err)
			}

			status := c.Response().Status
			span.SetAttributes(semconv.HTTPResponseStatusCode(status))
			if status >= 500 {
				span.SetStatus(codes.Error, httpStatusText(status))
			} else {
				span.SetStatus(codes.Unset, "")
			}
			if err != nil {
				span.RecordError(err)
			}
			return err
		}
	}
}

func requestScheme(c echo.Context) string {
	if c.Request().TLS != nil {
		return "https"
	}
	if proto := c.Request().Header.Get("X-Forwarded-Proto"); proto != "" {
		return proto
	}
	return "http"
}

func httpStatusText(code int) string {
	if code >= 500 {
		return fmt.Sprintf("HTTP %d", code)
	}
	return ""
}
