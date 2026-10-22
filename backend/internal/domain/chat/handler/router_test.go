// serveSPA 静态托管 / SPA fallback 单测。
package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// setupWebDir 在临时目录里造一个最小 web/ 目录并切到该目录（webFS 用相对路径 "web"）。
func setupWebDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	web := filepath.Join(dir, "web")
	if err := os.MkdirAll(filepath.Join(web, "assets"), 0o755); err != nil {
		t.Fatalf("mkdir web: %v", err)
	}
	files := map[string]string{
		"index.html":         "REDIRECT-STUB",
		"chat.html":          "PATIENT-ENTRY",
		"staff.html":         "STAFF-ENTRY",
		"assets/main-abc.js": "console.log(1)",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(web, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	t.Chdir(dir)
}

// serve 用 serveSPA 处理一次 GET，返回状态码与响应体。
func serve(t *testing.T, path string) (code int, body string) {
	t.Helper()
	rec := httptest.NewRecorder()
	serveSPA(rec, httptest.NewRequest(http.MethodGet, path, http.NoBody))
	return rec.Code, rec.Body.String()
}

// 根路径必须直接落到患者端入口：命中 index.html 中转页会多一次文档级导航（多一个 RTT）。
func TestServeSPA_RootServesPatientEntry(t *testing.T) {
	setupWebDir(t)
	code, body := serve(t, "/")
	if code != http.StatusOK || body != "PATIENT-ENTRY" {
		t.Errorf("GET /: code=%d body=%q, want 200 PATIENT-ENTRY", code, body)
	}
}

func TestServeSPA_ExistingAssetServed(t *testing.T) {
	setupWebDir(t)
	if code, body := serve(t, "/assets/main-abc.js"); code != http.StatusOK || body != "console.log(1)" {
		t.Errorf("GET /assets/main-abc.js: code=%d body=%q", code, body)
	}
}

// /staff/* 未匹配静态文件时回落到医护端入口，其余前端路由回落到患者端入口。
func TestServeSPA_FallbackByPrefix(t *testing.T) {
	setupWebDir(t)
	cases := map[string]string{
		"/staff/login":     "STAFF-ENTRY",
		"/staff/wiki/1":    "STAFF-ENTRY",
		"/chat":            "PATIENT-ENTRY",
		"/wiki/article/12": "PATIENT-ENTRY",
	}
	for path, want := range cases {
		if code, body := serve(t, path); code != http.StatusOK || body != want {
			t.Errorf("GET %s: code=%d body=%q, want 200 %q", path, code, body, want)
		}
	}
}
