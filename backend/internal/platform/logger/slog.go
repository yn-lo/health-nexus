// Package logger 提供应用级 slog.Logger，自动从 context 注入 request_id/trace_id/user_id。
package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"health-nexus/internal/shared/contextkeys"
)

// logDirPerm 日志目录权限位：owner 读写执行，group 读执行。
const logDirPerm = 0o750

// logFilePerm 日志文件权限位：仅 owner 读写。
const logFilePerm = 0o600

// requestIDHandler 包装 slog.Handler，Handle 时从 context 读取 request_id/user_id 写入日志属性。
type requestIDHandler struct {
	slog.Handler
}

// Handle 实现 slog.Handler，注入 trace_id/request_id/user_id 后委托给底层 handler。
// trace_id 与 request_id 复用同一值（不引入新 header），满足 REQ-NFR-019 链路追踪要求。
// user_id 来自 JWTAuth 写入的 ctx（int64），未鉴权请求（如 /healthz、公开端点）不注入 user_id。
func (h *requestIDHandler) Handle(ctx context.Context, r slog.Record) error {
	if rid := contextkeys.FromCtx(ctx, contextkeys.RequestID); rid != "" {
		// trace_id 与 request_id 复用：单进程内 request_id 即唯一追踪标识，
		// 跨进程链路如需独立 trace_id，未来可从 W3C Traceparent header 解析后覆盖。
		r.AddAttrs(slog.String("trace_id", rid), slog.String("request_id", rid))
	}
	if uid, ok := ctx.Value(contextkeys.UserID).(int64); ok && uid > 0 {
		r.AddAttrs(slog.Int64("user_id", uid))
	}
	return h.Handler.Handle(ctx, r)
}

// WithAttrs 实现 slog.Handler。
func (h *requestIDHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &requestIDHandler{Handler: h.Handler.WithAttrs(attrs)}
}

// WithGroup 实现 slog.Handler。
func (h *requestIDHandler) WithGroup(name string) slog.Handler {
	return &requestIDHandler{Handler: h.Handler.WithGroup(name)}
}

// New 创建 JSON 格式、Info 级别的 slog.Logger，并设为全局默认。
// 日志同时输出到 stdout 和 logs/app.log 文件。
// 文件写入失败不阻塞启动——仅输出警告到 stderr，日志流退化为 stdout only。
func New(logDir string) *slog.Logger {
	writers := []io.Writer{os.Stdout}
	if logDir != "" {
		if err := os.MkdirAll(logDir, logDirPerm); err == nil {
			f, err := os.OpenFile( // #nosec G304 -- 目录由部署方通过 logDir 参数指定，属可信配置
				filepath.Join(logDir, "app.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, logFilePerm,
			)
			if err == nil {
				writers = append(writers, f)
			} else {
				_, _ = os.Stderr.WriteString(
					"logger: failed to open log file, falling back to stdout only: " + err.Error() + "\n",
				)
			}
		} else {
			_, _ = os.Stderr.WriteString(
				"logger: failed to create log directory, falling back to stdout only: " + err.Error() + "\n",
			)
		}
	}
	base := slog.NewJSONHandler(io.MultiWriter(writers...), &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	log := slog.New(&requestIDHandler{Handler: base})
	slog.SetDefault(log)
	return log
}
