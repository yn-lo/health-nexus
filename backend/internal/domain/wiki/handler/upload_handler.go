package handler

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"

	"github.com/go-chi/chi/v5"

	apperrors "health-nexus/internal/shared/errors"
	"health-nexus/internal/shared/response"
)

// uploadURLPrefix 图片对外访问路径前缀（静态路由在 Router.Mount 中注册，匿名可访问）。
const uploadURLPrefix = "/uploads/"

// 上传约束默认值（配置缺省/非法时兜底）。
const (
	defaultUploadMaxSizeMB = 5
	fallbackUploadDir      = "uploads"
	bytesPerMB             = int64(1) << 20 // MB → 字节换算
	// maxFormOverhead multipart 表单边界/头部的额外字节余量，用于区分"图片超限"与"表单畸形"。
	maxFormOverhead = 1 << 20
	// maxUploadMemory multipart 解析的内存缓冲上限，超出部分落临时文件（请求结束由 net/http 清理）。
	maxUploadMemory = 1 << 20
	// dirPerm 存储目录权限；filePerm 存储文件权限（仅服务进程自身读写）。
	dirPerm  os.FileMode = 0o750
	filePerm os.FileMode = 0o600
)

// imageExtByContentType 由内容嗅探结果决定落盘扩展名。
// 不信任客户端文件名：既防伪造扩展名（.html/.svg 被当图片托管），也避免文件名穿越。
var imageExtByContentType = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

// reUploadName 上传文件名白名单：32 位小写 hex + 受支持扩展名。
// 严格匹配同时挡掉路径穿越（含 ".." 与路径分隔符），并让目录枚举无从下手。
var reUploadName = regexp.MustCompile(`^[0-9a-f]{32}\.(?:jpg|png|webp|gif)$`)

// UploadHandler 知识库文章正文图片上传（POST /api/staff/wiki/uploads）。
// 只做落盘 + 返回可引用 URL，不写业务表：图片与文章的关联完全由正文 HTML 中的 <img src> 承载，
// 图片本身不参与切片/向量化（切片前已剥离 img 标签，见 adapter.htmlToPlainText）。
//
// 访问控制说明：图片走随机文件名（128bit）构成的不可枚举 URL，
// 已发布文章的患者端可匿名加载，未发布草稿的图片也不会被目录枚举发现（未开目录列表）。
type UploadHandler struct {
	dir      string
	maxBytes int64
	maxMB    int
}

// NewUploadHandler 构造上传 handler。dir 为空时用 "uploads"，maxSizeMB<=0 时用 5。
func NewUploadHandler(dir string, maxSizeMB int) *UploadHandler {
	if dir == "" {
		dir = fallbackUploadDir
	}
	if maxSizeMB <= 0 {
		maxSizeMB = defaultUploadMaxSizeMB
	}
	return &UploadHandler{
		dir:      dir,
		maxBytes: int64(maxSizeMB) * bytesPerMB,
		maxMB:    maxSizeMB,
	}
}

// Create POST /api/staff/wiki/uploads — multipart 上传单张图片，返回 {url}。
// 校验顺序：表单可解析 → 存在 file 字段 → 非空 → 不超上限 → 内容确为受支持图片。
func (h *UploadHandler) Create(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, h.maxBytes+maxFormOverhead)
	// #nosec G120 -- 请求体已被 MaxBytesReader 限制为 maxBytes+overhead，解析规模有界
	if err := r.ParseMultipartForm(maxUploadMemory); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			response.WriteError(w, r, h.errTooLarge())
			return
		}
		response.WriteError(w, r, apperrors.Validation("WIKI_UPLOAD_INVALID_FORM", "上传表单格式错误"))
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		response.WriteError(w, r, apperrors.Validation("WIKI_UPLOAD_NO_FILE", "缺少 file 字段"))
		return
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(file)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			response.WriteError(w, r, h.errTooLarge())
			return
		}
		response.WriteError(w, r, apperrors.Internal("读取上传内容失败", err))
		return
	}
	if len(data) == 0 {
		response.WriteError(w, r, apperrors.Validation("WIKI_UPLOAD_EMPTY_FILE", "图片内容为空"))
		return
	}
	if int64(len(data)) > h.maxBytes {
		response.WriteError(w, r, h.errTooLarge())
		return
	}

	// 内容嗅探（读前 512 字节判定），扩展名由嗅探结果决定。
	ext, ok := imageExtByContentType[http.DetectContentType(data)]
	if !ok {
		response.WriteError(w, r, apperrors.Validation(
			"WIKI_UPLOAD_INVALID_TYPE", "仅支持 JPG/PNG/WebP/GIF 格式图片"))
		return
	}

	name, err := randomUploadName(ext)
	if err != nil {
		response.WriteError(w, r, apperrors.Internal("生成文件名失败", err))
		return
	}
	// 懒创建目录：首次上传时自动建目录，失败即视为服务端问题（目录不可写/磁盘满）。
	if err := os.MkdirAll(h.dir, dirPerm); err != nil {
		response.WriteError(w, r, apperrors.Internal("创建上传目录失败", err))
		return
	}
	if err := os.WriteFile(filepath.Join(h.dir, name), data, filePerm); err != nil {
		response.WriteError(w, r, apperrors.Internal("写入图片失败", err))
		return
	}

	slog.InfoContext(r.Context(), "wiki: article image uploaded", "name", name, "bytes", len(data))
	response.WriteCreated(w, map[string]any{"url": uploadURLPrefix + name})
}

// Serve GET /uploads/{filename} — 匿名读取已上传图片（患者端文章详情需要）。
// 仅接受白名单文件名，其余一律 400；白名单内但文件不存在返回 404（不做目录列表）。
// 返回 400 而非 404 的考量：非法名称是客户端错误，且与"路由不存在"区分开，便于契约测试断言路由可达。
func (h *UploadHandler) Serve(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "filename")
	if !reUploadName.MatchString(name) {
		response.WriteError(w, r, apperrors.BadRequest("WIKI_UPLOAD_INVALID_NAME", "图片名称无效"))
		return
	}
	fullPath := filepath.Join(h.dir, name)
	// #nosec G703 -- name 已通过 reUploadName 白名单（32位hex+受控扩展名），不含路径分隔符与 ..
	if _, err := os.Stat(fullPath); err != nil {
		response.WriteError(w, r, apperrors.NotFound("WIKI_UPLOAD_NOT_FOUND", "图片不存在"))
		return
	}
	// 文件名随机且内容不再变更，可长期强缓存。
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	// #nosec G703 -- name 已通过 reUploadName 白名单（32位hex+受控扩展名），不含路径分隔符与 ..
	http.ServeFile(w, r, fullPath)
}

// errTooLarge 构造 413（handler 层持有 net/http 常量，service/entity 层不感知状态码）。
func (h *UploadHandler) errTooLarge() *apperrors.AppError {
	return &apperrors.AppError{
		Code:    "WIKI_UPLOAD_TOO_LARGE",
		Message: fmt.Sprintf("图片超过 %dMB 上限", h.maxMB),
		HTTP:    http.StatusRequestEntityTooLarge,
	}
}

// randomUploadName 生成 32 位 hex 随机文件名 + 嗅探出的扩展名。
func randomUploadName(ext string) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf) + ext, nil
}
