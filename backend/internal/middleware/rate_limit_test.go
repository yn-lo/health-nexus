// Package middleware 限流器客户端 IP 解析单测（D-MED-02 修复）。
package middleware

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"health-nexus/internal/shared/contextkeys"
)

// trustedCIDRsForTest 测试用可信代理 CIDR（本地回环）。
var trustedCIDRsForTest = []string{"127.0.0.1/8", "::1/128"}

func TestClientIP_TrustedProxyTakesRightmostUntrusted(t *testing.T) {
	// RemoteAddr 在 trustedProxies 内，XFF 含多跳——从右向左跳过可信，返回最右侧不可信 IP。
	rl := NewRateLimiter(nil, trustedCIDRsForTest)
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.RemoteAddr = "127.0.0.1:54321"
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 127.0.0.1")
	if got := clientIP(r, rl.trustedProxies); got != "1.2.3.4" {
		t.Errorf("trusted proxy + XFF chain: got %q, want 1.2.3.4", got)
	}
}

func TestClientIP_UntrustedRemoteIgnoresXFF(t *testing.T) {
	// RemoteAddr 不在 trustedProxies 内 → 忽略 XFF，返回 RemoteAddr IP（防伪造绕过）。
	rl := NewRateLimiter(nil, trustedCIDRsForTest)
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.RemoteAddr = "8.8.8.8:54321"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := clientIP(r, rl.trustedProxies); got != "8.8.8.8" {
		t.Errorf("untrusted remote + XFF: got %q, want 8.8.8.8 (XFF must be ignored)", got)
	}
}

func TestClientIP_TrustedProxyNoXFF(t *testing.T) {
	// RemoteAddr 可信但无 XFF → 返回 RemoteAddr IP。
	rl := NewRateLimiter(nil, trustedCIDRsForTest)
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.RemoteAddr = "127.0.0.1:54321"
	if got := clientIP(r, rl.trustedProxies); got != "127.0.0.1" {
		t.Errorf("trusted remote + no XFF: got %q, want 127.0.0.1", got)
	}
}

func TestClientIP_EmptyTrustedProxiesIgnoresXFF(t *testing.T) {
	// 空可信代理列表 → 永远用 RemoteAddr，XFF 完全被忽略（最严格模式）。
	rl := NewRateLimiter(nil, nil)
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.RemoteAddr = "127.0.0.1:54321"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := clientIP(r, rl.trustedProxies); got != "127.0.0.1" {
		t.Errorf("empty trusted proxies: got %q, want 127.0.0.1", got)
	}
}

func TestClientIP_AllTrustedChainReturnsLeftmost(t *testing.T) {
	// XFF 全部为可信 IP（链路全在可信网段）→ 返回最左侧非空 IP。
	rl := NewRateLimiter(nil, trustedCIDRsForTest)
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.RemoteAddr = "127.0.0.1:54321"
	r.Header.Set("X-Forwarded-For", "127.0.0.1, 127.0.0.2")
	if got := clientIP(r, rl.trustedProxies); got != "127.0.0.1" {
		t.Errorf("all-trusted chain: got %q, want 127.0.0.1 (leftmost)", got)
	}
}

func TestBuildRateKey_GlobalScopeSharesBucket(t *testing.T) {
	// global: 前缀 scope 下不同设备共享同一桶（防批量伪造 device_id 绕过限流）。
	r1 := httptest.NewRequest(http.MethodPost, "/", http.NoBody)
	r1 = r1.WithContext(context.WithValue(r1.Context(), contextkeys.DeviceID, "device-aaa"))
	r2 := httptest.NewRequest(http.MethodPost, "/", http.NoBody)
	r2 = r2.WithContext(context.WithValue(r2.Context(), contextkeys.DeviceID, "device-bbb"))

	k1 := buildRateKey(r1, "global:chat_stream_anon", nil)
	k2 := buildRateKey(r2, "global:chat_stream_anon", nil)
	if k1 != k2 {
		t.Errorf("global scope should share bucket: %q vs %q", k1, k2)
	}
	if k1 != "rate:global:chat_stream_anon" {
		t.Errorf("unexpected global key: %q", k1)
	}
	// 非 global scope 仍按设备区分。
	if k := buildRateKey(r1, "chat_stream_anon", nil); k != "rate:chat_stream_anon:device-aaa" {
		t.Errorf("per-device key: got %q", k)
	}
}

func TestParseTrustedProxies_SingleIPAutoMask(t *testing.T) {
	// 单 IP（无 /前缀）应自动补全为 /32 或 /128；非法条目静默跳过。
	cidrs := parseTrustedProxies([]string{"127.0.0.1", "::1", "10.0.0.0/8", "  ", "invalid"})
	if len(cidrs) != 3 {
		t.Fatalf("expected 3 valid CIDRs, got %d", len(cidrs))
	}
	// 验证 127.0.0.1（补全为 /32）能匹配
	if !cidrs[0].Contains(net.ParseIP("127.0.0.1")) {
		t.Errorf("127.0.0.1/32 should contain 127.0.0.1")
	}
}
