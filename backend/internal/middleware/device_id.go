package middleware

import (
	"context"
	"net/http"
	"regexp"

	"health-nexus/internal/shared/contextkeys"
	apperrors "health-nexus/internal/shared/errors"
	"health-nexus/internal/shared/response"
)

// deviceIDPattern 设备 ID 合法格式：8-64 位，仅允许字母、数字、连字符、下划线。
// 前端生成 UUID v4（36 位）满足此约束；校验防止超长/特殊字符污染限流 Redis key。
var deviceIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

// RequireDeviceID 从 X-Device-Id 请求头提取匿名设备标识，写入 context。
// 头缺失、为空或格式非法时返回 400。
func RequireDeviceID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deviceID := r.Header.Get("X-Device-Id")
		if deviceID == "" {
			response.WriteError(w, r, apperrors.BadRequest("MISSING_DEVICE_ID", "缺少 X-Device-Id 请求头"))
			return
		}
		if !deviceIDPattern.MatchString(deviceID) {
			response.WriteError(w, r, apperrors.BadRequest("INVALID_DEVICE_ID", "X-Device-Id 格式非法"))
			return
		}
		ctx := context.WithValue(r.Context(), contextkeys.DeviceID, deviceID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
