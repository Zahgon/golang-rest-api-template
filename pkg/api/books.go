package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"golang-rest-api-template/pkg/cache"
	"golang-rest-api-template/pkg/httperr"
	"golang-rest-api-template/pkg/httpresp"
	"golang-rest-api-template/pkg/middleware"
	"golang-rest-api-template/pkg/models"
	"golang-rest-api-template/pkg/repository"
	"golang-rest-api-template/pkg/service"

	"github.com/labstack/echo/v4"
)

const (
	findBooksMinLimit = 1
	findBooksMaxLimit = 100
)

// BookHandler defines Echo handlers for book routes (HTTP layer only; persistence
// lives in pkg/repository).
type BookHandler interface {
	Healthcheck(c echo.Context) error
	FindBooks(c echo.Context) error
	CreateBook(c echo.Context) error
	FindBook(c echo.Context) error
	UpdateBook(c echo.Context) error
	PatchBook(c echo.Context) error
	DeleteBook(c echo.Context) error
}

type bookHandler struct {
	svc *service.BookService
}

// NewBookHandler wires persistence and cache into book HTTP handlers.
func NewBookHandler(store repository.BookPersistence, redisClient cache.Cache) *bookHandler {
	return &bookHandler{svc: service.NewBookService(store, redisClient)}
}

// defaultQuery returns the query parameter named key, or fallback when it is absent.
func defaultQuery(c echo.Context, key, fallback string) string {
	if _, ok := c.QueryParams()[key]; !ok {
		return fallback
	}
	return c.QueryParam(key)
}

// parseIDParam reads the :id path parameter. When it is not a valid id, the
// error response is written, ok is false, and the returned error is whatever
// writing that response produced.
func parseIDParam(c echo.Context) (id uint, ok bool, err error) {
	if c == nil {
		return 0, false, nil
	}
	value := c.Param("id")
	parsed, parseErr := strconv.ParseUint(value, 10, strconv.IntSize)
	if parseErr != nil {
		return 0, false, httperr.Write(c, http.StatusBadRequest, "Invalid id format")
	}
	return uint(parsed), true, nil
}

func parseOffsetLimit(c echo.Context) (offset, limit int, ok bool, err error) {
	offsetQuery := strings.TrimSpace(defaultQuery(c, "offset", "0"))
	limitQuery := strings.TrimSpace(defaultQuery(c, "limit", "10"))

	o, parseErr := strconv.Atoi(offsetQuery)
	if parseErr != nil {
		return 0, 0, false, httperr.Write(c, http.StatusBadRequest, "Invalid offset format")
	}
	l, parseErr := strconv.Atoi(limitQuery)
	if parseErr != nil {
		return 0, 0, false, httperr.Write(c, http.StatusBadRequest, "Invalid limit format")
	}
	if o < 0 {
		return 0, 0, false, httperr.Write(c, http.StatusBadRequest, "offset must be >= 0")
	}
	if l < findBooksMinLimit {
		return 0, 0, false, httperr.Write(c, http.StatusBadRequest, "limit must be at least 1")
	}
	if l > findBooksMaxLimit {
		l = findBooksMaxLimit
	}
	return o, l, true, nil
}

// parseBookListQuery parses pagination, filter, and sort query params for GET /books.
func parseBookListQuery(c echo.Context) (repository.BookListQuery, bool, error) {
	var q repository.BookListQuery
	offset, limit, ok, err := parseOffsetLimit(c)
	if !ok {
		return q, false, err
	}
	q.Offset = offset
	q.Limit = limit
	q.TitleLike = strings.TrimSpace(c.QueryParam("title_like"))
	q.AuthorLike = strings.TrimSpace(c.QueryParam("author_like"))

	ownerRaw := strings.TrimSpace(c.QueryParam("owner_id"))
	if ownerRaw != "" {
		id, parseErr := strconv.ParseUint(ownerRaw, 10, strconv.IntSize)
		if parseErr != nil {
			return q, false, httperr.Write(c, http.StatusBadRequest, "Invalid owner_id format")
		}
		ownerID := uint(id)
		q.OwnerID = &ownerID
	}

	sort := strings.ToLower(strings.TrimSpace(defaultQuery(c, "sort", "id")))
	if _, allowed := repository.BookListSortFields[sort]; !allowed {
		return q, false, httperr.Write(c, http.StatusBadRequest, "Invalid sort field")
	}
	q.Sort = sort

	order := strings.ToLower(strings.TrimSpace(defaultQuery(c, "order", "asc")))
	if order != "asc" && order != "desc" {
		return q, false, httperr.Write(c, http.StatusBadRequest, "Invalid order; must be asc or desc")
	}
	q.Order = order
	return q, true, nil
}

// contextUserID returns the authenticated users.id set by middleware.JWTAuth.
func contextUserID(c echo.Context) (uint, bool) {
	if c == nil {
		return 0, false
	}
	id, ok := c.Get(middleware.ContextUserID).(uint)
	return id, ok && id > 0
}

// @BasePath /api/v1

// Healthcheck godoc
// @Summary ping example
// @Schemes
// @Description do ping
// @Tags example
// @Accept json
// @Produce json
// @Success 200 {object} models.HealthOKBody "Health payload in standard envelope"
// @Router / [get]
func (h *bookHandler) Healthcheck(c echo.Context) error {
	return httpresp.OK(c, "ok")
}

// FindBooks godoc
// @Summary Get all books with pagination, filter, and sort
// @Description Get a list of books with optional pagination (offset/limit), filters (title_like, author_like, owner_id), and sort (sort/order). List entries are keyed by a monotonic cache generation (no Redis KEYS) and a canonical query after parsing (leading zeros and surrounding whitespace in query params do not fragment the cache). Concurrent cache misses for the same query and generation are coalesced (singleflight) so only one database read and Redis write runs per cache key.
// @Tags books
// @Security ApiKeyAuth
// @Produce json
// @Param offset query int false "Offset for pagination (must be >= 0)" default(0)
// @Param limit query int false "Limit for pagination (minimum 1, capped at 100)" default(10)
// @Param title_like query string false "Case-insensitive substring filter on title"
// @Param author_like query string false "Case-insensitive substring filter on author"
// @Param owner_id query int false "Exact match filter on owner_id"
// @Param sort query string false "Sort field (id, title, author, created_at, updated_at, owner_id)" default(id) Enums(id, title, author, created_at, updated_at, owner_id)
// @Param order query string false "Sort direction" default(asc) Enums(asc, desc)
// @Success 200 {array} models.Book "Successfully retrieved list of books"
// @Failure 400 {string} string "Bad Request"
// @Failure 500 {string} string "Internal Server Error"
// @Router /books [get]
func (h *bookHandler) FindBooks(c echo.Context) error {
	q, ok, err := parseBookListQuery(c)
	if !ok {
		return err
	}
	books, err := h.svc.ListBooks(c.Request().Context(), q)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrListBooksDB):
			return httperr.Write(c, http.StatusInternalServerError, "Failed to list books")
		case errors.Is(err, service.ErrListBooksMarshal):
			return httperr.Write(c, http.StatusInternalServerError, "Failed to marshal data")
		case errors.Is(err, service.ErrListBooksRedis):
			return httperr.Write(c, http.StatusInternalServerError, "Failed to set cache")
		case errors.Is(err, service.ErrListBooksUnmarshal):
			return httperr.Write(c, http.StatusInternalServerError, "Failed to unmarshal cached data")
		default:
			return httperr.Write(c, http.StatusInternalServerError, "Failed to list books")
		}
	}
	return httpresp.OK(c, books)
}

// CreateBook godoc
// @Summary Create a new book
// @Description Create a new book with the given input data
// @Tags books
// @Security ApiKeyAuth
// @Security JwtAuth
// @Accept  json
// @Produce  json
// @Param   input     body   models.CreateBook   true   "Create book object"
// @Success 201 {object} models.Book "Successfully created book"
// @Failure 400 {string} string "Bad Request"
// @Failure 401 {string} string "Unauthorized"
// @Failure 500 {string} string "Internal Server Error"
// @Router /books [post]
func (h *bookHandler) CreateBook(c echo.Context) error {
	ownerID, ok := contextUserID(c)
	if !ok {
		return httperr.Write(c, http.StatusUnauthorized, "Unauthorized")
	}
	var input models.CreateBook
	if err := bindJSON(c, &input); err != nil {
		return httperr.Write(c, http.StatusBadRequest, err.Error())
	}
	book, err := h.svc.CreateBook(c.Request().Context(), ownerID, input.Title, input.Author)
	if err != nil {
		return httperr.Write(c, http.StatusInternalServerError, "Failed to create book")
	}
	return httpresp.Created(c, book)
}

// FindBook godoc
// @Summary Find a book by ID
// @Description Get details of a book by its ID
// @Tags books
// @Security ApiKeyAuth
// @Produce json
// @Param id path string true "Book ID"
// @Success 200 {object} models.Book "Successfully retrieved book"
// @Failure 404 {string} string "Book not found"
// @Router /books/{id} [get]
func (h *bookHandler) FindBook(c echo.Context) error {
	id, ok, err := parseIDParam(c)
	if !ok {
		return err
	}
	book, err := h.svc.GetBook(c.Request().Context(), id)
	if err != nil {
		if repository.IsBookNotFound(err) {
			return httperr.Write(c, http.StatusNotFound, "book not found")
		}
		return httperr.Write(c, http.StatusInternalServerError, "Failed to load book")
	}
	return httpresp.OK(c, book)
}

// UpdateBook godoc
// @Summary Replace a book by ID (PUT)
// @Description Replaces both title and author. Use PATCH for partial updates.
// @Tags books
// @Security ApiKeyAuth
// @Security JwtAuth
// @Accept  json
// @Produce  json
// @Param id path string true "Book ID"
// @Param input body models.ReplaceBook true "Full book fields"
// @Success 200 {object} models.Book "Successfully updated book"
// @Failure 400 {string} string "Bad Request"
// @Failure 401 {string} string "Unauthorized"
// @Failure 403 {string} string "Forbidden"
// @Failure 404 {string} string "book not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /books/{id} [put]
func (h *bookHandler) UpdateBook(c echo.Context) error {
	actorID, ok := contextUserID(c)
	if !ok {
		return httperr.Write(c, http.StatusUnauthorized, "Unauthorized")
	}
	var input models.ReplaceBook
	id, ok, err := parseIDParam(c)
	if !ok {
		return err
	}
	if err := bindJSON(c, &input); err != nil {
		return httperr.Write(c, http.StatusBadRequest, err.Error())
	}
	book, err := h.svc.ReplaceBook(c.Request().Context(), actorID, id, input.Title, input.Author)
	if err != nil {
		if repository.IsBookNotFound(err) {
			return httperr.Write(c, http.StatusNotFound, "book not found")
		}
		if errors.Is(err, service.ErrBookForbidden) {
			return httperr.Write(c, http.StatusForbidden, "forbidden")
		}
		return httperr.Write(c, http.StatusInternalServerError, "Failed to update book")
	}
	return httpresp.OK(c, book)
}

// PatchBook godoc
// @Summary Partially update a book by ID (PATCH)
// @Description Updates only fields present in the JSON body (at least one of title or author).
// @Tags books
// @Security ApiKeyAuth
// @Security JwtAuth
// @Accept  json
// @Produce  json
// @Param id path string true "Book ID"
// @Param input body models.PatchBook true "Fields to change"
// @Success 200 {object} models.Book "Successfully updated book"
// @Failure 400 {string} string "Bad Request"
// @Failure 401 {string} string "Unauthorized"
// @Failure 403 {string} string "Forbidden"
// @Failure 404 {string} string "book not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /books/{id} [patch]
func (h *bookHandler) PatchBook(c echo.Context) error {
	actorID, ok := contextUserID(c)
	if !ok {
		return httperr.Write(c, http.StatusUnauthorized, "Unauthorized")
	}
	id, ok, err := parseIDParam(c)
	if !ok {
		return err
	}
	var input models.PatchBook
	if err := bindJSON(c, &input); err != nil {
		return httperr.Write(c, http.StatusBadRequest, err.Error())
	}
	if input.Title == nil && input.Author == nil {
		return httperr.Write(c, http.StatusBadRequest, "at least one of title or author is required")
	}
	book, err := h.svc.PatchBook(c.Request().Context(), actorID, id, input.Title, input.Author)
	if err != nil {
		if repository.IsBookNotFound(err) {
			return httperr.Write(c, http.StatusNotFound, "book not found")
		}
		if errors.Is(err, service.ErrBookForbidden) {
			return httperr.Write(c, http.StatusForbidden, "forbidden")
		}
		return httperr.Write(c, http.StatusInternalServerError, "Failed to update book")
	}
	return httpresp.OK(c, book)
}

// DeleteBook godoc
// @Summary Delete a book by ID
// @Description Delete the book with the given ID
// @Tags books
// @Security ApiKeyAuth
// @Security JwtAuth
// @Produce json
// @Param id path string true "Book ID"
// @Success 204 "Successfully deleted book"
// @Failure 401 {string} string "Unauthorized"
// @Failure 403 {string} string "Forbidden"
// @Failure 404 {string} string "book not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /books/{id} [delete]
func (h *bookHandler) DeleteBook(c echo.Context) error {
	actorID, ok := contextUserID(c)
	if !ok {
		return httperr.Write(c, http.StatusUnauthorized, "Unauthorized")
	}
	id, ok, err := parseIDParam(c)
	if !ok {
		return err
	}
	if err := h.svc.DeleteBook(c.Request().Context(), actorID, id); err != nil {
		if repository.IsBookNotFound(err) {
			return httperr.Write(c, http.StatusNotFound, "book not found")
		}
		if errors.Is(err, service.ErrBookForbidden) {
			return httperr.Write(c, http.StatusForbidden, "forbidden")
		}
		return httperr.Write(c, http.StatusInternalServerError, "Failed to delete book")
	}
	return c.NoContent(http.StatusNoContent)
}
