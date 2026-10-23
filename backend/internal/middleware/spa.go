package middleware

import (
	"net/http"
	"strings"
)

// 包级静态资源句柄：避免每次请求重建 FileServer。
// ponytail: 相对路径依赖工作目录（WORKDIR=/app，web 目录位于 /app/web），
// 与 Dockerfile 部署约定一致。升级路径：通过 config 注入绝对路径。
var (
	webFS       = http.Dir("web")
	staticFiles = http.FileServer(webFS)
)

// ServeSPA 托管前端静态文件与 SPA fallback（同源部署）。
// 先尝试静态文件（含 /assets/、index.html 等），不存在则按路径前缀返回对应 SPA 入口：
//   - /staff/* 或 /styles → web/staff.html
//   - 其他路径（/chat、/wiki/article/:id、/about、/terms、/privacy 等）→ web/chat.html
//
// 根路径直接落到患者端入口：dist/index.html 只是跳转页，命中它会多一次文档级导航。
func ServeSPA(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		http.ServeFile(w, r, "web/chat.html")
		return
	}
	if f, err := webFS.Open(r.URL.Path); err == nil {
		f.Close()
		staticFiles.ServeHTTP(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/staff") || r.URL.Path == "/styles" {
		http.ServeFile(w, r, "web/staff.html")
		return
	}
	http.ServeFile(w, r, "web/chat.html")
}
