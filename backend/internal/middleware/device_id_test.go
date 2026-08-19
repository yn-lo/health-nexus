// Package middleware RequireDeviceID 格式校验单测。
package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"health-nexus/internal/shared/contextkeys"
)

// runRequireDeviceID 记录请求是否到达下游并捕获 ctx 中的 device_id。
// 返回 HTTP 状态码与下游捕获到的 device_id。
func runRequireDeviceID(deviceID string) (httpCode int, deviceIDCaptured string) {
	var gotID string
	h := RequireDeviceID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID, _ = r.Context().Value(contextkeys.DeviceID).(string)
		w.WriteHeader(http.StatusOK)
	}))
	r := httptest.NewRequest(http.MethodPost, "/", http.NoBody)
	if deviceID != "" {
		r.Header.Set("X-Device-Id", deviceID)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec.Code, gotID
}

func TestRequireDeviceID_ValidUUID(t *testing.T) {
	code, id := runRequireDeviceID("550e8400-e29b-41d4-a716-446655440000")
	if code != http.StatusOK || id != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("valid UUID: code=%d id=%q", code, id)
	}
}

func TestRequireDeviceID_MissingHeader(t *testing.T) {
	if code, _ := runRequireDeviceID(""); code != http.StatusBadRequest {
		t.Errorf("missing header: code=%d, want 400", code)
	}
}

func TestRequireDeviceID_InvalidFormat(t *testing.T) {
	cases := []string{
		"short",                     // 过短（<8）
		"包含中文的设备标识符哦",               // 非 ASCII 字符
		"device id with spaces 123", // 空格
		"a:b:c:d:e",                 // 冒号（Redis key 分隔符，防 key 结构污染）
		"abc\n\rinjection",          // 控制字符
		"012345678901234567890123456789012345678901234567890123456789012345", // 过长（>64）
	}
	for _, c := range cases {
		if code, _ := runRequireDeviceID(c); code != http.StatusBadRequest {
			t.Errorf("device_id %q: code=%d, want 400", c, code)
		}
	}
}
