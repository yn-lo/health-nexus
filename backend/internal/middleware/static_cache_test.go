// Package middleware StaticCache 缓存策略单测。
package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// runStaticCache 返回指定路径下 StaticCache 下发的 Cache-Control 响应头。
func runStaticCache(path string) string {
	h := StaticCache(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, http.NoBody))
	return rec.Header().Get("Cache-Control")
}

func TestStaticCache_HashedAssetsImmutable(t *testing.T) {
	const want = "public, max-age=31536000, immutable"
	if got := runStaticCache("/assets/main-DFJxpPbA.js"); got != want {
		t.Errorf("/assets/*: Cache-Control=%q, want %q", got, want)
	}
}

func TestStaticCache_HTMLNoCache(t *testing.T) {
	for _, p := range []string{"/chat", "/staff/login", "/", "/chat.html"} {
		if got := runStaticCache(p); got != "no-cache" {
			t.Errorf("%s: Cache-Control=%q, want no-cache", p, got)
		}
	}
}

func TestStaticCache_HandlerOwnedPathsUntouched(t *testing.T) {
	// /api/* 与 /uploads/* 由各自 handler 决定，中间件不得覆盖（例如 upload 的 immutable）。
	for _, p := range []string{"/api/public/brand", "/uploads/a.png"} {
		if got := runStaticCache(p); got != "" {
			t.Errorf("%s: Cache-Control=%q, want empty (handler owns it)", p, got)
		}
	}
}
