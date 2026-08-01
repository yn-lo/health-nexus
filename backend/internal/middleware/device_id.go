package middleware

import (
	"context"
	"net/http"

	"health-nexus/internal/shared/contextkeys"
	apperrors "health-nexus/internal/shared/errors"
	"health-nexus/internal/shared/response"
)

// RequireDeviceID 从 X-Device-Id 请求头提取匿名设备标识，写入 context。
// 头缺失或为空时返回 400。
func RequireDeviceID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deviceID := r.Header.Get("X-Device-Id")
		if deviceID == "" {
			response.WriteError(w, r, apperrors.BadRequest("MISSING_DEVICE_ID", "缺少 X-Device-Id 请求头"))
			return
		}
		ctx := context.WithValue(r.Context(), contextkeys.DeviceID, deviceID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
