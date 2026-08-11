package gateway

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"autozeagent.local/autozeagent/internal/applicationerror"
	"autozeagent.local/autozeagent/internal/corequery"
)

type listRequest struct {
	Page corequery.Page
	Sort corequery.SortDirection
}

type pageMetadata struct {
	Limit    int    `json:"limit"`
	Offset   int    `json:"offset"`
	Returned int    `json:"returned"`
	Order    string `json:"order"`
}

func (request listRequest) metadata(returned int) pageMetadata {
	return pageMetadata{
		Limit: request.Page.Limit, Offset: request.Page.Offset, Returned: returned, Order: string(request.Sort),
	}
}

func parseListRequest(w http.ResponseWriter, r *http.Request) (listRequest, bool) {
	limit, ok := parseLimit(w, r)
	if !ok {
		return listRequest{}, false
	}
	offset := 0
	if value := strings.TrimSpace(r.URL.Query().Get("offset")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 {
			writeError(w, http.StatusBadRequest, "invalid_offset", "offset must be a non-negative integer")
			return listRequest{}, false
		}
		offset = parsed
	}
	order := corequery.SortDescending
	if value := strings.TrimSpace(r.URL.Query().Get("order")); value != "" {
		order = corequery.SortDirection(strings.ToLower(value))
		if order != corequery.SortAscending && order != corequery.SortDescending {
			writeError(w, http.StatusBadRequest, "invalid_order", "order must be asc or desc")
			return listRequest{}, false
		}
	}
	return listRequest{Page: corequery.Page{Limit: limit, Offset: offset}, Sort: order}, true
}

func parseLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	value := strings.TrimSpace(r.URL.Query().Get("limit"))
	if value == "" {
		return defaultListLimit, true
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit <= 0 || limit > maximumListLimit {
		writeError(w, http.StatusBadRequest, "invalid_limit", "limit must be between 1 and 500")
		return 0, false
	}
	return limit, true
}

func parseAfter(w http.ResponseWriter, r *http.Request) (uint64, bool) {
	value := strings.TrimSpace(r.URL.Query().Get("after"))
	if value == "" {
		return 0, true
	}
	after, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_after", "after must be an unsigned integer")
		return 0, false
	}
	return after, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("invalid JSON request")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request must contain one JSON value")
	}
	return nil
}

func pathID(w http.ResponseWriter, path, prefix string) (string, bool) {
	id := strings.TrimSpace(strings.TrimPrefix(path, prefix))
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
		return "", false
	}
	return id, true
}

func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	methodNotAllowed(w, method)
	return false
}
func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
}

type errorResponse struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

func writeApplicationError(w http.ResponseWriter, err error) bool {
	code, ok := applicationerror.CodeOf(err)
	if !ok {
		return false
	}
	status := 0
	message := ""
	switch code {
	case applicationerror.CodeInvalidRequest:
		status, message = http.StatusBadRequest, "invalid request"
	case applicationerror.CodeNotFound:
		status, message = http.StatusNotFound, "resource not found"
	case applicationerror.CodeConflict:
		status, message = http.StatusConflict, "request conflicts with current state"
	case applicationerror.CodeUnavailable:
		status, message = http.StatusServiceUnavailable, "service temporarily unavailable"
	default:
		return false
	}
	writeErrorWithRetryability(w, status, string(code), message, applicationerror.IsRetryable(err))
	return true
}

func writeInternal(w http.ResponseWriter, err error) {
	if err != nil {
		slog.Error("gateway internal error", "component", "gateway", "operation", "http", "result", "failed", "error", err)
	}
	writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeErrorWithRetryability(w, status, code, message, false)
}
func writeErrorWithRetryability(w http.ResponseWriter, status int, code, message string, retryable bool) {
	writeJSON(w, status, errorResponse{Error: errorDetail{Code: code, Message: message, Retryable: retryable}})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func randomID(prefix string) (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(value), nil
}
