// 上传 handler 单测：内容嗅探、大小上限、文件名不可控、静态读取白名单。
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// pngSig / jpegSig 最小可被 http.DetectContentType 识别的图片头。
var (
	pngSig  = []byte("\x89PNG\r\n\x1a\n")
	jpegSig = []byte("\xff\xd8\xff\xe0")
)

// newMultipartRequest 构造上传请求（fieldName 为表单字段名）。
func newMultipartRequest(t *testing.T, fieldName, filename string, content []byte) *http.Request {
	t.Helper()
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	if fieldName != "" {
		fw, err := mw.CreateFormFile(fieldName, filename)
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := fw.Write(content); err != nil {
			t.Fatalf("write form file: %v", err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/staff/wiki/uploads", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

// errCodeOf 解析错误响应体中的 code 字段。
func errCodeOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	code, _ := body["code"].(string)
	return code
}

// ============================================================================
// Create：上传
// ============================================================================

func TestUploadHandler_Create_Success(t *testing.T) {
	dir := t.TempDir()
	h := NewUploadHandler(dir, 5)

	content := append(append([]byte{}, pngSig...), []byte(strings.Repeat("a", 128))...)
	rec := httptest.NewRecorder()
	h.Create(rec, newMultipartRequest(t, "file", "screenshot.png", content))

	if rec.Code != http.StatusCreated {
		t.Fatalf("期望 201，实际 %d（body=%s）", rec.Code, rec.Body.String())
	}
	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// 文件名必须为 32 位随机 hex + 嗅探出的扩展名。
	if !regexp.MustCompile(`^/uploads/[0-9a-f]{32}\.png$`).MatchString(body.URL) {
		t.Fatalf("URL 形态不符合预期：%q", body.URL)
	}
	// 落盘内容与上传一致。
	saved, err := os.ReadFile(filepath.Join(dir, filepath.Base(body.URL)))
	if err != nil {
		t.Fatalf("读取落盘文件失败: %v", err)
	}
	if !bytes.Equal(saved, content) {
		t.Error("落盘内容与上传内容不一致")
	}
}

func TestUploadHandler_Create_IgnoresClientFilename(t *testing.T) {
	dir := t.TempDir()
	h := NewUploadHandler(dir, 5)

	// 客户端伪造扩展名 + 路径穿越文件名：扩展名必须由内容嗅探决定。
	rec := httptest.NewRecorder()
	h.Create(rec, newMultipartRequest(t, "file", "../../evil.html", pngSig))

	if rec.Code != http.StatusCreated {
		t.Fatalf("期望 201，实际 %d", rec.Code)
	}
	var body struct {
		URL string `json:"url"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if !strings.HasSuffix(body.URL, ".png") {
		t.Errorf("期望按内容嗅探落为 .png，实际 %q", body.URL)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("期望仅落盘 1 个文件，实际 %d", len(entries))
	}
	if entries[0].Name() != filepath.Base(body.URL) {
		t.Errorf("落盘文件名 %q 与返回 URL %q 不一致", entries[0].Name(), body.URL)
	}
}

func TestUploadHandler_Create_UnsupportedType(t *testing.T) {
	h := NewUploadHandler(t.TempDir(), 5)

	rec := httptest.NewRecorder()
	h.Create(rec, newMultipartRequest(t, "file", "note.txt", []byte("这不是图片，只是文本内容")))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("期望 422，实际 %d", rec.Code)
	}
	if code := errCodeOf(t, rec); code != "WIKI_UPLOAD_INVALID_TYPE" {
		t.Errorf("期望 WIKI_UPLOAD_INVALID_TYPE，实际 %q", code)
	}
}

func TestUploadHandler_Create_TooLarge(t *testing.T) {
	dir := t.TempDir()
	h := NewUploadHandler(dir, 1) // 上限 1MB

	oversized := append(append([]byte{}, jpegSig...), bytes.Repeat([]byte("a"), 1<<20)...)
	rec := httptest.NewRecorder()
	h.Create(rec, newMultipartRequest(t, "file", "big.jpg", oversized))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("期望 413，实际 %d", rec.Code)
	}
	if code := errCodeOf(t, rec); code != "WIKI_UPLOAD_TOO_LARGE" {
		t.Errorf("期望 WIKI_UPLOAD_TOO_LARGE，实际 %q", code)
	}
	// 超限不得落盘。
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("期望不落盘，实际 %d 个文件", len(entries))
	}
}

func TestUploadHandler_Create_MissingFileField(t *testing.T) {
	h := NewUploadHandler(t.TempDir(), 5)

	rec := httptest.NewRecorder()
	h.Create(rec, newMultipartRequest(t, "", "", nil))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("期望 422，实际 %d", rec.Code)
	}
	if code := errCodeOf(t, rec); code != "WIKI_UPLOAD_NO_FILE" {
		t.Errorf("期望 WIKI_UPLOAD_NO_FILE，实际 %q", code)
	}
}

func TestUploadHandler_Create_MalformedForm(t *testing.T) {
	h := NewUploadHandler(t.TempDir(), 5)

	req := httptest.NewRequest(http.MethodPost, "/api/staff/wiki/uploads", strings.NewReader("not-a-multipart-body"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=xxx")
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("期望 422，实际 %d", rec.Code)
	}
	if code := errCodeOf(t, rec); code != "WIKI_UPLOAD_INVALID_FORM" {
		t.Errorf("期望 WIKI_UPLOAD_INVALID_FORM，实际 %q", code)
	}
}

// ============================================================================
// Serve：静态读取
// ============================================================================

// uploadTestRouter 构造挂载 Serve 的最小路由（chi URL 参数需要真实路由）。
func uploadTestRouter(h *UploadHandler) http.Handler {
	r := chi.NewRouter()
	r.Get("/uploads/{filename}", h.Serve)
	return r
}

func TestUploadHandler_Serve(t *testing.T) {
	dir := t.TempDir()
	h := NewUploadHandler(dir, 5)
	router := uploadTestRouter(h)

	// 先上传一张，拿到真实文件名。
	rec := httptest.NewRecorder()
	h.Create(rec, newMultipartRequest(t, "file", "a.png", pngSig))
	var body struct {
		URL string `json:"url"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&body)

	t.Run("命中_返回图片", func(t *testing.T) {
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, body.URL, http.NoBody))
		if rr.Code != http.StatusOK {
			t.Fatalf("期望 200，实际 %d", rr.Code)
		}
		if ct := rr.Header().Get("Content-Type"); ct != "image/png" {
			t.Errorf("期望 Content-Type image/png，实际 %q", ct)
		}
		if cc := rr.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
			t.Errorf("期望强缓存响应头，实际 %q", cc)
		}
	})

	t.Run("文件名非法_返回400", func(t *testing.T) {
		for _, name := range []string{"notahexname.png", "abc.png", strings.Repeat("a", 32) + ".svg"} {
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/uploads/"+name, http.NoBody))
			if rr.Code != http.StatusBadRequest {
				t.Errorf("%s：期望 400，实际 %d", name, rr.Code)
			}
		}
	})

	t.Run("路径穿越_返回400", func(t *testing.T) {
		// 路由参数直接注入穿越文件名，验证白名单校验而非路由兜底拦截。
		for _, name := range []string{"../config.yaml", "../../etc/passwd", filepath.Join("..", "a.png")} {
			req := httptest.NewRequest(http.MethodGet, "/uploads/x", http.NoBody)
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("filename", name)
			rr := httptest.NewRecorder()
			h.Serve(rr, req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx)))
			if rr.Code != http.StatusBadRequest {
				t.Errorf("%s：期望 400，拒绝路径穿越，实际 %d", name, rr.Code)
			}
		}
	})

	t.Run("文件不存在_返回404", func(t *testing.T) {
		missing := "/uploads/" + strings.Repeat("a", 32) + ".png"
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, missing, http.NoBody))
		if rr.Code != http.StatusNotFound {
			t.Errorf("期望 404，实际 %d", rr.Code)
		}
	})
}
