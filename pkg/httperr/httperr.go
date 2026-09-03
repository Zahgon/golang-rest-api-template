package httperr

import "github.com/labstack/echo/v4"

// ErrorBody is the standard JSON error envelope for this API: {"error":"..."}.
type ErrorBody struct {
	Error string `json:"error"`
}

// Write responds with a JSON error body and the given HTTP status. The caller
// should return immediately after; handlers keep returning to Echo normally.
func Write(c echo.Context, status int, message string) error {
	if c == nil {
		return nil
	}
	return c.JSON(status, ErrorBody{Error: message})
}

// Abort responds with a JSON error body and sets the HTTP status. Use in
// middleware: returning without calling the next handler stops the Echo chain,
// so no later handlers run.
func Abort(c echo.Context, status int, message string) error {
	if c == nil {
		return nil
	}
	return c.JSON(status, ErrorBody{Error: message})
}
