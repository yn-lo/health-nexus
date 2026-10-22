package middleware

import (
	"net/http"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// compressLevel gzip 压缩级别：5 = 压缩率/CPU 的折中（chi 推荐值）。
const compressLevel = 5

// gzip 压缩的响应类型白名单。
//
// 显式列举而非用 chi 默认列表，是为了把 SSE 的 text/event-stream 排除在外：
// 压缩后每次写入都要等 gzip 缓冲，流式问答会从"逐字下发"退化成"攒一波再发"。
var compressibleTypes = []string{
	"text/html",
	"text/css",
	"text/javascript",
	"application/javascript",
	"application/json",
	"image/svg+xml",
}

// Compress 对静态资源与 JSON 响应做 gzip 压缩（患者端首屏 JS+CSS 395KB → ~137KB）。
func Compress(next http.Handler) http.Handler {
	return chimw.Compress(compressLevel, compressibleTypes...)(next)
}
