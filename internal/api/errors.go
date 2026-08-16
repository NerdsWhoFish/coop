package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/nerdswhofish/coop/internal/auth"
	"github.com/nerdswhofish/coop/internal/store"
	"github.com/nerdswhofish/coop/internal/youtube"
)

// errorBody is the shape every failure takes, matching api/openapi.yaml.
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// apiError is a failure with a chosen status and client-safe message.
type apiError struct {
	status  int
	code    string
	message string
	cause   error
}

func (e *apiError) Error() string {
	if e.cause != nil {
		return e.code + ": " + e.cause.Error()
	}
	return e.code + ": " + e.message
}

func (e *apiError) Unwrap() error { return e.cause }

func badRequest(message string) *apiError {
	return &apiError{status: http.StatusBadRequest, code: "bad_request", message: message}
}

func unauthorized() *apiError {
	return &apiError{
		status:  http.StatusUnauthorized,
		code:    "unauthorized",
		message: "missing or invalid credentials",
	}
}

func forbidden(message string) *apiError {
	return &apiError{status: http.StatusForbidden, code: "forbidden", message: message}
}

// notFound is also the answer for anything outside the caller's scope, so a
// response never confirms that a resource it may not see exists.
func notFound() *apiError {
	return &apiError{status: http.StatusNotFound, code: "not_found", message: "not found"}
}

func conflict(code, message string) *apiError {
	return &apiError{status: http.StatusConflict, code: code, message: message}
}

func tooManyRequests(code, message string) *apiError {
	return &apiError{status: http.StatusTooManyRequests, code: code, message: message}
}

func internal(err error) *apiError {
	return &apiError{
		status:  http.StatusInternalServerError,
		code:    "internal",
		message: "something went wrong",
		cause:   err,
	}
}

// toAPIError maps a domain error onto a response, so handlers can return the
// error they got rather than restating the mapping at every call site.
func toAPIError(err error) *apiError {
	var already *apiError
	if errors.As(err, &already) {
		return already
	}

	switch {
	case errors.Is(err, store.ErrNotFound), errors.Is(err, auth.ErrOutOfScope):
		return notFound()
	case errors.Is(err, auth.ErrNotAdmin):
		return forbidden("this action requires an admin parent")
	case errors.Is(err, store.ErrLastAdmin):
		return conflict("last_admin", "a family must keep at least one admin")
	case errors.Is(err, youtube.ErrBudgetExhausted):
		return tooManyRequests("quota_exhausted",
			"the daily YouTube API budget is used up, try again tomorrow")
	default:
		return internal(err)
	}
}

// writeJSON renders a successful response.
func writeJSON(w http.ResponseWriter, logger *slog.Logger, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	if body == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// The status line is already sent, so this can only be logged.
		logger.Error("encoding response body", "error", err)
	}
}

// writeError renders a failure, logging the cause without leaking it.
func writeError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	apiErr := toAPIError(err)

	if apiErr.status >= http.StatusInternalServerError {
		logger.Error("request failed",
			"method", r.Method, "path", r.URL.Path,
			"status", apiErr.status, "error", apiErr.Error())
	} else {
		logger.Debug("request rejected",
			"method", r.Method, "path", r.URL.Path,
			"status", apiErr.status, "code", apiErr.code)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(apiErr.status)
	_ = json.NewEncoder(w).Encode(errorBody{Code: apiErr.code, Message: apiErr.message})
}

// decodeJSON reads a request body, rejecting unknown fields so a typo in a
// client payload fails loudly instead of being silently ignored.
func decodeJSON(r *http.Request, dest any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dest); err != nil {
		return badRequest("malformed request body: " + err.Error())
	}
	return nil
}
