// Package middleware Compress 白名单单测。
package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// runCompress 以指定 Content-Type 穿过 Compress 中间件，返回响应头与解码后的响应体。
func runCompress(contentType, body string) (encoding, got string) {
	h := Compress(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}))
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	encoding = rec.Header().Get("Content-Encoding")
	if encoding != "gzip" {
		return encoding, rec.Body.String()
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		return encoding, ""
	}
	defer func() { _ = zr.Close() }()
	decoded, _ := io.ReadAll(zr)
	return encoding, string(decoded)
}

func TestCompress_StaticAndJSON(t *testing.T) {
	body := strings.Repeat(`{"code":"OK"}`, 50)
	for _, ct := range []string{
		"text/html", "text/css", "text/javascript",
		"application/javascript", "application/json", "image/svg+xml",
	} {
		encoding, got := runCompress(ct, body)
		if encoding != "gzip" {
			t.Errorf("%s: Content-Encoding=%q, want gzip", ct, encoding)
		}
		if got != body {
			t.Errorf("%s: body mismatch after gzip round-trip", ct)
		}
	}
}

// SSE 必须原样透传：一旦被 gzip 缓冲，流式问答就不是逐字下发而是攒批下发。
func TestCompress_SSEPassthrough(t *testing.T) {
	chunk := "data: {\"delta\":\"你好\"}\n\n"
	encoding, got := runCompress("text/event-stream", chunk)
	if encoding != "" {
		t.Errorf("SSE: Content-Encoding=%q, want empty (must not be compressed)", encoding)
	}
	if got != chunk {
		t.Errorf("SSE: body=%q, want %q", got, chunk)
	}
}

// SSE 的 Flush 必须直达底层 ResponseWriter，否则前端收不到中间的心跳。
func TestCompress_SSEFlushReachesUnderlying(t *testing.T) {
	flushed := false
	underlying := &flushRecorder{ResponseRecorder: httptest.NewRecorder(), onFlush: func() { flushed = true }}
	h := Compress(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	h.ServeHTTP(underlying, httptest.NewRequest(http.MethodPost, "/", http.NoBody))
	if !flushed {
		t.Error("SSE: Flush 未透传到下层 ResponseWriter")
	}
}

// flushRecorder 记录 Flush 调用次数。
type flushRecorder struct {
	*httptest.ResponseRecorder
	onFlush func()
}

func (f *flushRecorder) Flush() {
	f.onFlush()
	f.ResponseRecorder.Flush()
}
