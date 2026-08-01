// DataIsolation 中间件单元测试（REQ-SEC-003）。
// 覆盖 DataIsolation 中间件对 ctx 中身份字段的采集与 DataScope 注入，
// 以及 ScopeFromCtx 在挂载/未挂载中间件时的行为。
package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"health-nexus/internal/shared/contextkeys"
)

// ============================================================================
// DataIsolation 中间件：从 ctx 采集身份字段并注入 DataScope
// ============================================================================

func TestDataIsolation_FillsDataScope(t *testing.T) {
	t.Run("ctx含UserID/UserRole/DeptID_字段正确填充", func(t *testing.T) {
		// 预置 ctx 的身份字段（模拟 JWTAuth 写入）
		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		ctx := context.WithValue(req.Context(), contextkeys.UserID, int64(42))
		ctx = context.WithValue(ctx, contextkeys.UserRole, "DOCTOR")
		ctx = context.WithValue(ctx, contextkeys.DeptID, int64(7))
		req = req.WithContext(ctx)

		var captured *DataScope
		mw := DataIsolation()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			captured = ScopeFromCtx(r.Context())
		}))
		mw.ServeHTTP(httptest.NewRecorder(), req)

		if captured == nil {
			t.Fatal("期望 DataScope 非 nil")
		}
		if captured.UserID != 42 {
			t.Errorf("UserID = %d, want 42", captured.UserID)
		}
		if captured.Role != "DOCTOR" {
			t.Errorf("Role = %q, want %q", captured.Role, "DOCTOR")
		}
		if captured.DeptID != 7 {
			t.Errorf("DeptID = %d, want 7", captured.DeptID)
		}
	})

	t.Run("ctx无任何字段_DataScope全零值但不nil", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)

		var captured *DataScope
		mw := DataIsolation()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			captured = ScopeFromCtx(r.Context())
		}))
		mw.ServeHTTP(httptest.NewRecorder(), req)

		if captured == nil {
			t.Fatal("期望 DataScope 非 nil（即使无字段也应注入空 DataScope）")
		}
		if captured.UserID != 0 {
			t.Errorf("UserID 期望 0，实际 %d", captured.UserID)
		}
		if captured.Role != "" {
			t.Errorf("Role 期望空，实际 %q", captured.Role)
		}
		if captured.DeptID != 0 {
			t.Errorf("DeptID 期望 0，实际 %d", captured.DeptID)
		}
		if captured.TokenType != "" {
			t.Errorf("TokenType 期望空（中间件当前不填充），实际 %q", captured.TokenType)
		}
	})

	t.Run("部分字段缺失_仅填充存在的字段", func(t *testing.T) {
		// 只设置 UserID，不设置 Role/DeptID
		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		ctx := context.WithValue(req.Context(), contextkeys.UserID, int64(99))
		req = req.WithContext(ctx)

		var captured *DataScope
		mw := DataIsolation()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			captured = ScopeFromCtx(r.Context())
		}))
		mw.ServeHTTP(httptest.NewRecorder(), req)

		if captured == nil {
			t.Fatal("期望 DataScope 非 nil")
		}
		if captured.UserID != 99 {
			t.Errorf("UserID = %d, want 99", captured.UserID)
		}
		if captured.Role != "" {
			t.Errorf("Role 期望空（未设置），实际 %q", captured.Role)
		}
		if captured.DeptID != 0 {
			t.Errorf("DeptID 期望 0（未设置），实际 %d", captured.DeptID)
		}
	})

	t.Run("字段类型不匹配_静默跳过不panic", func(t *testing.T) {
		// 故意写入错误类型（string 而非 int64），中间件类型断言失败应跳过
		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		ctx := context.WithValue(req.Context(), contextkeys.UserID, "not-an-int64") // 错误类型
		ctx = context.WithValue(ctx, contextkeys.UserRole, 123)                     // 错误类型（int 而非 string）
		req = req.WithContext(ctx)

		var captured *DataScope
		mw := DataIsolation()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			captured = ScopeFromCtx(r.Context())
		}))
		mw.ServeHTTP(httptest.NewRecorder(), req)

		if captured == nil {
			t.Fatal("期望 DataScope 非 nil")
		}
		// 类型不匹配的字段应保持零值
		if captured.UserID != 0 {
			t.Errorf("UserID 期望 0（类型不匹配），实际 %d", captured.UserID)
		}
		if captured.Role != "" {
			t.Errorf("Role 期望空（类型不匹配），实际 %q", captured.Role)
		}
	})

	t.Run("中间件后next_handler能通过ScopeFromCtx取到DataScope", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		ctx := context.WithValue(req.Context(), contextkeys.UserID, int64(1))
		req = req.WithContext(ctx)

		handlerCalled := false
		mw := DataIsolation()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope := ScopeFromCtx(r.Context())
			if scope == nil {
				t.Error("next handler 中 ScopeFromCtx 返回 nil")
				return
			}
			if scope.UserID != 1 {
				t.Errorf("UserID = %d, want 1", scope.UserID)
			}
			handlerCalled = true
		}))
		mw.ServeHTTP(httptest.NewRecorder(), req)

		if !handlerCalled {
			t.Error("next handler 未被调用")
		}
	})
}

// ============================================================================
// ScopeFromCtx：从未挂载中间件的 ctx 读取应返回 nil
// ============================================================================

func TestScopeFromCtx(t *testing.T) {
	t.Run("未挂载中间件_返回nil", func(t *testing.T) {
		// 普通空 ctx，从未经过 DataIsolation 中间件
		ctx := context.Background()
		got := ScopeFromCtx(ctx)
		if got != nil {
			t.Errorf("期望 nil，实际 %+v", got)
		}
	})

	t.Run("挂载后_返回非nil指针", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		mw := DataIsolation()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			got := ScopeFromCtx(r.Context())
			if got == nil {
				t.Error("期望非 nil")
			}
		}))
		mw.ServeHTTP(httptest.NewRecorder(), req)
	})

	t.Run("ctx存有其他键值_不影响ScopeFromCtx", func(t *testing.T) {
		// ctx 含其他 key（非 DataScopeKey），ScopeFromCtx 应返回 nil
		ctx := context.WithValue(context.Background(), contextkeys.RequestID, "req-123")
		got := ScopeFromCtx(ctx)
		if got != nil {
			t.Errorf("期望 nil（仅 RequestID 不应被识别为 DataScope），实际 %+v", got)
		}
	})
}
