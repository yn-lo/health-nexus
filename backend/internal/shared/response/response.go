// Package response 封装 HTTP JSON 响应和错误响应。
package response

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"health-nexus/internal/shared/contextkeys"
	apperrors "health-nexus/internal/shared/errors"
)

// WriteJSON 写入 JSON 响应。
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// WriteOK 写入 200 响应。
func WriteOK(w http.ResponseWriter, v any) {
	WriteJSON(w, http.StatusOK, v)
}

// WriteCreated 写入 201 响应。
func WriteCreated(w http.ResponseWriter, v any) {
	WriteJSON(w, http.StatusCreated, v)
}

// WriteNoContent 写入 204 响应。
func WriteNoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// WriteError 统一错误响应。AppError 提取 HTTP+Code，未知错误统一 500。
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) {
		WriteJSON(w, appErr.HTTP, map[string]any{
			"code":    appErr.Code,
			"message": appErr.Message,
		})
		return
	}
	// 未知错误：仅入日志，响应 500
	rid := contextkeys.FromCtx(r.Context(), contextkeys.RequestID)
	slog.Error("unhandled error", "err", err, "request_id", rid)
	WriteJSON(w, http.StatusInternalServerError, map[string]any{
		"code":    "INTERNAL_ERROR",
		"message": "服务器内部错误",
	})
}
