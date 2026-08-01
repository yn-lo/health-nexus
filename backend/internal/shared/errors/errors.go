// Package errors 定义统一业务错误模型 AppError。
// Handler 层用 errors.As 提取 HTTP 状态码和业务错误码。
package errors

import (
	"fmt"
	"net/http"
)

// AppError 业务错误，携带 HTTP 状态码和业务错误码。
type AppError struct {
	Code    string // 业务错误码，如 "AUTH_INVALID_CREDENTIALS"
	Message string // 用户可读消息（不暴露内部细节）
	HTTP    int    // HTTP 状态码
	Cause   error  // 原始错误（仅日志记录，不序列化到响应）
}

func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s (cause: %v)", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error { return e.Cause }

// 常用错误构造器（按 HTTP 状态码分组）。
// 内部使用 net/http 状态码常量；service/repository/entity 层通过这些构造器
// 间接引用状态码，满足 AC-ARCH-03（service 层不得 import net/http）。

func BadRequest(code, msg string) *AppError {
	return &AppError{Code: code, Message: msg, HTTP: http.StatusBadRequest}
}

func Unauthorized(code, msg string) *AppError {
	return &AppError{Code: code, Message: msg, HTTP: http.StatusUnauthorized}
}

func Forbidden(code, msg string) *AppError {
	return &AppError{Code: code, Message: msg, HTTP: http.StatusForbidden}
}

func NotFound(code, msg string) *AppError {
	return &AppError{Code: code, Message: msg, HTTP: http.StatusNotFound}
}

func Conflict(code, msg string) *AppError {
	return &AppError{Code: code, Message: msg, HTTP: http.StatusConflict}
}

func Locked(code, msg string) *AppError {
	return &AppError{Code: code, Message: msg, HTTP: http.StatusLocked}
}

func Validation(code, msg string) *AppError {
	return &AppError{Code: code, Message: msg, HTTP: http.StatusUnprocessableEntity}
}

func RateLimited(code, msg string) *AppError {
	return &AppError{Code: code, Message: msg, HTTP: http.StatusTooManyRequests}
}

func Internal(msg string, cause error) *AppError {
	return &AppError{Code: "INTERNAL_ERROR", Message: msg, HTTP: http.StatusInternalServerError, Cause: cause}
}

func ServiceUnavailable(code, msg string) *AppError {
	return &AppError{Code: code, Message: msg, HTTP: http.StatusServiceUnavailable}
}
