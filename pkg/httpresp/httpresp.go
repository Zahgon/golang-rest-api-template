package httpresp

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

type envelope[T any] struct {
	Data T `json:"data"`
}

// OK writes HTTP 200 with the standard JSON envelope {"data": <payload>}.
func OK[T any](c echo.Context, data T) error {
	if c == nil {
		return nil
	}
	return c.JSON(http.StatusOK, envelope[T]{Data: data})
}

// Created writes HTTP 201 with the standard JSON envelope {"data": <payload>}.
func Created[T any](c echo.Context, data T) error {
	if c == nil {
		return nil
	}
	return c.JSON(http.StatusCreated, envelope[T]{Data: data})
}
