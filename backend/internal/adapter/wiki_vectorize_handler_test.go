// wiki_vectorize_handler 单元测试：覆盖 chunkContent 纯函数与 HandleVectorize asynq handler。
// 白盒测试（package adapter）以访问未导出的 chunkContent。
package adapter

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"health-nexus/internal/domain/wiki/entity"
	"health-nexus/internal/domain/wiki/repository"
	wikiservice "health-nexus/internal/domain/wiki/service"
	"health-nexus/internal/shared/constants"
	"health-nexus/internal/shared/contenthash"

	asynqlib "github.com/hibiken/asynq"
)

// ============================================================================
// 测试辅助：mock 实现
// ============================================================================

// mockArticleFetcher 模拟 articleFetcher 接口。
type mockArticleFetcher struct {
	article *entity.Article
	err     error
	lastID  int64
	called  bool
	// lockVersion 非 0 时 LockVersion 返回该值（模拟 embedding 期间文章已被更新）；
	// lockErr 非 nil 时 LockVersion 返回该错误。
	lockVersion int
	lockErr     error
	lockCalled  bool
}

func (m *mockArticleFetcher) GetByID(_ context.Context, id int64) (*entity.Article, error) {
	m.called = true
	m.lastID = id
	if m.err != nil {
		return nil, m.err
	}
	return m.article, nil
}

func (m *mockArticleFetcher) LockVersion(_ context.Context, _ int64) (int, error) {
	m.lockCalled = true
	if m.lockErr != nil {
		return 0, m.lockErr
	}
	if m.lockVersion != 0 {
		return m.lockVersion, nil
	}
	if m.article != nil {
		return m.article.Version, nil
	}
	return 0, nil
}

// mockChunkWriter 模拟 chunkWriter 接口，记录调用以供断言。
type mockChunkWriter struct {
	deactivateErr      error
	createErr          error
	deleteInactiveErr  error
	deactivatedID      int64
	deactivateCall     int
	deleteInactiveID   int64
	deleteInactiveCall int
	createdChunks      []*entity.ArticleChunk
}

func (m *mockChunkWriter) DeactivateByArticle(_ context.Context, articleID int64) (int64, error) {
	m.deactivateCall++
	m.deactivatedID = articleID
	if m.deactivateErr != nil {
		return 0, m.deactivateErr
	}
	return 0, nil
}

func (m *mockChunkWriter) DeleteInactiveByArticle(_ context.Context, articleID int64) (int64, error) {
	m.deleteInactiveCall++
	m.deleteInactiveID = articleID
	if m.deleteInactiveErr != nil {
		return 0, m.deleteInactiveErr
	}
	return 0, nil
}

func (m *mockChunkWriter) Create(_ context.Context, c *entity.ArticleChunk) error {
	m.createdChunks = append(m.createdChunks, c)
	if m.createErr != nil {
		return m.createErr
	}
	return nil
}

// mockEmbedder 模拟 llm.Embedder 接口。
type mockEmbedder struct {
	vectors [][]float32
	err     error
	called  bool
	lastN   int
}

func (m *mockEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	m.called = true
	m.lastN = len(texts)
	if m.err != nil {
		return nil, m.err
	}
	return m.vectors, nil
}

// mockModelEmbedder 额外暴露 EmbeddingModel（验证切片记录向量模型版本）。
type mockModelEmbedder struct {
	mockEmbedder
	model string
}

func (m *mockModelEmbedder) EmbeddingModel() string { return m.model }

// switchingModelEmbedder 模拟 Embedding 模型热切换：Embed 调用时把当前模型名切到 onEmbed，
// 用于验证切片记录的模型标识取"任务开始时的快照"而非提交时的当前模型。
type switchingModelEmbedder struct {
	mockEmbedder
	model   string
	onEmbed string
}

func (m *switchingModelEmbedder) EmbeddingModel() string { return m.model }

func (m *switchingModelEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	m.model = m.onEmbed // 热切换：Embedding 期间当前模型已变为新模型
	return m.mockEmbedder.Embed(ctx, texts)
}

// EmbedWithModel 模拟真实客户端（*SwappableClient）：单次调用内固定客户端快照，
// 用**切换前**的模型生成向量并返回同一模型名，二者必然一致。
func (m *switchingModelEmbedder) EmbedWithModel(
	ctx context.Context, texts []string,
) (embeddings [][]float32, model string, err error) {
	modelAtStart := m.model // 快照：本次调用使用的模型
	embeddings, err = m.mockEmbedder.Embed(ctx, texts)
	m.model = m.onEmbed // 调用结束后才发生热切换
	if err != nil {
		return nil, "", err
	}
	return embeddings, modelAtStart, nil
}

// mockTxRunner 模拟 TxRunner，直接同步执行 fn 并记录调用。
type mockTxRunner struct {
	called bool
}

func (m *mockTxRunner) WithTx(_ context.Context, fn func(ctx context.Context) error) error {
	m.called = true
	return fn(context.Background())
}

// mockConfigProvider 模拟 wikiservice.RAGConfigProvider，返回预设的切片配置。
type mockConfigProvider struct {
	cfg *wikiservice.RAGSearchConfig
	err error
}

func (m *mockConfigProvider) GetRAGConfig(_ context.Context) (*wikiservice.RAGSearchConfig, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.cfg, nil
}

// makeTask 构造 asynq.Task（payload 为 articleID 的字符串形式）。
func makeTask(payload string) *asynqlib.Task {
	return asynqlib.NewTask("wiki:vectorize", []byte(payload))
}

// ============================================================================
// chunkContent：纯函数测试
// ============================================================================

func TestChunkContent(t *testing.T) {
	cases := []struct {
		name    string
		content string
		size    int
		overlap int
		want    []string
	}{
		{
			name:    "空内容_返回nil",
			content: "",
			size:    10,
			overlap: 2,
			want:    nil,
		},
		{
			name:    "纯空白字符_返回nil",
			content: "   \n\t  ",
			size:    10,
			overlap: 2,
			want:    nil,
		},
		{
			name:    "纯空格_返回nil",
			content: "     ",
			size:    10,
			overlap: 2,
			want:    nil,
		},
		{
			name:    "纯换行_返回nil",
			content: "\n\n\n",
			size:    10,
			overlap: 2,
			want:    nil,
		},
		{
			name:    "size非正_返回nil",
			content: "abc",
			size:    0,
			overlap: 1,
			want:    nil,
		},
		{
			name:    "短内容_小于chunkSize_单chunk",
			content: "abc",
			size:    10,
			overlap: 2,
			want:    []string{"abc"},
		},
		{
			name:    "内容等于chunkSize_单chunk",
			content: "abcde",
			size:    5,
			overlap: 2,
			want:    []string{"abcde"},
		},
		{
			name:    "内容略大于chunkSize_两chunk带overlap",
			content: "abcdefgh",
			size:    5,
			overlap: 2,
			// step=3: [0:5]="abcde", [3:8]="defgh"
			want: []string{"abcde", "defgh"},
		},
		{
			name:    "overlap大于等于chunkSize_强制为0",
			content: "abcdef",
			size:    3,
			overlap: 3,
			// overlap→0, step=3: [0:3]="abc", [3:6]="def"
			want: []string{"abc", "def"},
		},
		{
			name:    "overlap远大于chunkSize_强制为0",
			content: "abcdef",
			size:    3,
			overlap: 100,
			want:    []string{"abc", "def"},
		},
		{
			name:    "overlap为负数_强制为0",
			content: "abcdef",
			size:    3,
			overlap: -1,
			want:    []string{"abc", "def"},
		},
		{
			name:    "中文rune切片_chunkSize3_overlap1",
			content: "你好世界测试文本",
			size:    3,
			overlap: 1,
			// runes: 你好世界测试文本 (8 runes)
			// step=2: [0:3]="你好世", [2:5]="世界测", [4:7]="测试文", [6:8]="文本"
			want: []string{"你好世", "世界测", "测试文", "文本"},
		},
		{
			name:    "中文rune切片_验证非字节切片",
			content: "你好", // 6 bytes, 2 runes
			size:    5,
			overlap: 1,
			want:    []string{"你好"}, // 2 runes < 5 → 单 chunk
		},
		{
			name:    "overlap为0_无重叠",
			content: "abcdef",
			size:    2,
			overlap: 0,
			// step=2: [0:2], [2:4], [4:6]
			want: []string{"ab", "cd", "ef"},
		},
		{
			name:    "末尾不足size_取剩余部分",
			content: "abcdefg",
			size:    3,
			overlap: 0,
			// step=3: [0:3]="abc", [3:6]="def", [6:7]="g"
			want: []string{"abc", "def", "g"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := chunkContent(tc.content, tc.size, tc.overlap)
			if len(got) != len(tc.want) {
				t.Fatalf("期望 %d 个 chunk %v，实际 %d 个 %v",
					len(tc.want), tc.want, len(got), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("chunk[%d] = %q，期望 %q", i, got[i], tc.want[i])
				}
			}
		})
	}

	// 大内容验证：切片连续且覆盖完整内容。
	t.Run("大内容_切片连续且覆盖完整内容", func(t *testing.T) {
		const totalRunes = 1000
		const size = 50
		const overlap = 10
		content := strings.Repeat("abcdefghijklmnopqrstuvwxyz", totalRunes/26+1)
		content = string([]rune(content)[:totalRunes])

		got := chunkContent(content, size, overlap)
		if len(got) == 0 {
			t.Fatal("期望非空切片")
		}

		// 验证每个 chunk 长度不超过 size（最后一个可更短）。
		for i, c := range got {
			if rc := len([]rune(c)); rc > size {
				t.Errorf("chunk[%d] 长度 %d 超过 size %d", i, rc, size)
			}
		}

		// 验证相邻 chunk 的 overlap 一致性。
		for i := 1; i < len(got); i++ {
			// 校验：当前 chunk 前 overlap 个 rune 应等于上一 chunk 末尾 overlap 个 rune
			prevRunes := []rune(got[i-1])
			curRunes := []rune(got[i])
			// 上一 chunk 长度应为 size（除最后一个），当前 chunk 的前 overlap 个应等于上一 chunk 的后 overlap 个
			if i < len(got)-1 || len(prevRunes) == size {
				// 非末尾相邻对，或上一 chunk 长度==size 时校验 overlap
				if len(prevRunes) >= overlap && len(curRunes) >= overlap {
					prevTail := string(prevRunes[len(prevRunes)-overlap:])
					curHead := string(curRunes[:overlap])
					if prevTail != curHead {
						t.Errorf("chunk[%d] 头部 %q 与 chunk[%d] 尾部 %q 不一致（期望 overlap=%d）",
							i, curHead, i-1, prevTail, overlap)
					}
				}
			}
		}

		// 验证：拼合所有 chunk（去掉每个后续 chunk 的前 overlap 个 rune）能还原原文。
		reconstructed := reconstructChunks(got, overlap)
		if reconstructed != content {
			t.Errorf("重构内容与原文不一致（重构长度 %d，原文长度 %d）",
				len([]rune(reconstructed)), len([]rune(content)))
		}
	})
}

// reconstructChunks 通过去除每个后续 chunk 的前 overlap 个 rune 拼合原文。
func reconstructChunks(chunks []string, overlap int) string {
	if len(chunks) == 0 {
		return ""
	}
	runes := []rune(chunks[0])
	for i := 1; i < len(chunks); i++ {
		next := []rune(chunks[i])
		start := overlap
		if start > len(next) {
			start = len(next)
		}
		runes = append(runes, next[start:]...)
	}
	return string(runes)
}

// ============================================================================
// htmlToPlainText：富文本 → 纯文本（切片输入）
// ============================================================================

func TestHTMLToPlainText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "纯文本原样返回", in: "高血压患者日常管理", want: "高血压患者日常管理"},
		{name: "空字符串", in: "", want: ""},
		{name: "纯空白", in: "  \n\t ", want: ""},
		{
			name: "块级标签转为分隔不粘连",
			in:   "<p>甲</p><p>乙</p>",
			want: "甲 乙",
		},
		{
			name: "行内标签直接丢弃",
			in:   "<p>血压<strong>偏高</strong>时就医</p>",
			want: "血压偏高时就医",
		},
		{
			name: "img标签及其URL不进入文本",
			in:   `<p>用药说明</p><img src="https://cdn.example.com/a.png" alt="图"><p>遵医嘱</p>`,
			want: "用药说明 遵医嘱",
		},
		{
			name: "标题与列表保留语义边界",
			in:   "<h2>注意事项</h2><ul><li>低盐</li><li>运动</li></ul>",
			want: "注意事项 低盐 运动",
		},
		{
			name: "HTML实体被解码",
			in:   "<p>结论是 &quot;低盐饮食&quot;&nbsp;有效</p>",
			want: `结论是 "低盐饮食" 有效`,
		},
		{
			name: "br换行转为分隔",
			in:   "第一行<br>第二行",
			want: "第一行 第二行",
		},
		{
			name: "仅图片_无文本",
			in:   `<p><img src="https://cdn.example.com/a.png"></p>`,
			want: "",
		},
		{
			name: "带属性的div与span",
			in:   `<div class="x" style="color:red"><span>内容</span></div>`,
			want: "内容",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := htmlToPlainText(tc.in); got != tc.want {
				t.Errorf("htmlToPlainText(%q) = %q，期望 %q", tc.in, got, tc.want)
			}
		})
	}
}

// ============================================================================
// HandleVectorize：handler 流程测试
// ============================================================================

func TestHandleVectorize(t *testing.T) {
	t.Run("无效payload_返回SkipRetry", func(t *testing.T) {
		h := &VectorizeHandler{
			articles: &mockArticleFetcher{},
			chunks:   &mockChunkWriter{},
			embed:    &mockEmbedder{},
		}
		err := h.HandleVectorize(context.Background(), makeTask("not-a-number"))
		if err == nil {
			t.Fatal("期望 error 非 nil")
		}
		if !errors.Is(err, asynqlib.SkipRetry) {
			t.Errorf("期望错误包含 SkipRetry，实际 %v", err)
		}
	})

	t.Run("空payload_返回SkipRetry", func(t *testing.T) {
		h := &VectorizeHandler{
			articles: &mockArticleFetcher{},
			chunks:   &mockChunkWriter{},
			embed:    &mockEmbedder{},
		}
		err := h.HandleVectorize(context.Background(), makeTask(""))
		if err == nil {
			t.Fatal("期望 error 非 nil")
		}
		if !errors.Is(err, asynqlib.SkipRetry) {
			t.Errorf("期望错误包含 SkipRetry，实际 %v", err)
		}
	})

	t.Run("文章不存在_返回SkipRetry", func(t *testing.T) {
		fetcher := &mockArticleFetcher{err: repository.ErrNotFound}
		h := &VectorizeHandler{articles: fetcher, chunks: &mockChunkWriter{}, embed: &mockEmbedder{}}

		err := h.HandleVectorize(context.Background(), makeTask("42"))
		if err == nil {
			t.Fatal("期望 error 非 nil")
		}
		if !errors.Is(err, asynqlib.SkipRetry) {
			t.Errorf("期望错误包含 SkipRetry（文章不存在不重试），实际 %v", err)
		}
		if fetcher.lastID != 42 {
			t.Errorf("期望 GetByID 被调用 with id=42，实际 %d", fetcher.lastID)
		}
	})

	t.Run("GetByID返回其他错误_返回可重试错误", func(t *testing.T) {
		// DB 错误不应返回 SkipRetry，应触发 asynq 重试。
		dbErr := errors.New("db connection lost")
		h := &VectorizeHandler{
			articles: &mockArticleFetcher{err: dbErr},
			chunks:   &mockChunkWriter{},
			embed:    &mockEmbedder{},
		}
		err := h.HandleVectorize(context.Background(), makeTask("1"))
		if err == nil {
			t.Fatal("期望 error 非 nil")
		}
		if errors.Is(err, asynqlib.SkipRetry) {
			t.Errorf("期望错误不含 SkipRetry（DB 错误应可重试），实际 %v", err)
		}
		if !errors.Is(err, dbErr) {
			t.Errorf("期望错误包装原始 dbErr，实际 %v", err)
		}
	})

	t.Run("文章未发布_返回SkipRetry", func(t *testing.T) {
		fetcher := &mockArticleFetcher{article: &entity.Article{
			ID:      7,
			Status:  constants.ArticleStatusDraft,
			Content: "hello",
			Version: 1,
		}}
		embed := &mockEmbedder{}
		chunks := &mockChunkWriter{}
		h := &VectorizeHandler{articles: fetcher, chunks: chunks, embed: embed}

		err := h.HandleVectorize(context.Background(), makeTask("7"))
		if err == nil {
			t.Fatal("期望 error 非 nil")
		}
		if !errors.Is(err, asynqlib.SkipRetry) {
			t.Errorf("期望错误包含 SkipRetry（未发布不重试），实际 %v", err)
		}
		// 未发布时不应调用 embed 或写 chunk
		if embed.called {
			t.Error("期望 embed 未被调用")
		}
		if chunks.deactivateCall != 0 {
			t.Error("期望 DeactivateByArticle 未被调用")
		}
		if len(chunks.createdChunks) != 0 {
			t.Errorf("期望未创建任何 chunk，实际 %d", len(chunks.createdChunks))
		}
	})

	t.Run("文章已归档_返回SkipRetry", func(t *testing.T) {
		h := &VectorizeHandler{
			articles: &mockArticleFetcher{article: &entity.Article{
				ID:      8,
				Status:  constants.ArticleStatusArchived,
				Content: "hello",
				Version: 1,
			}},
			chunks: &mockChunkWriter{},
			embed:  &mockEmbedder{},
		}
		err := h.HandleVectorize(context.Background(), makeTask("8"))
		if err == nil {
			t.Fatal("期望 error 非 nil")
		}
		if !errors.Is(err, asynqlib.SkipRetry) {
			t.Errorf("期望错误包含 SkipRetry（已归档不重试），实际 %v", err)
		}
	})

	t.Run("成功向量化_切片写入正确", func(t *testing.T) {
		// 构造内容使其产生 2 个 chunk（chunk_size=800, content=1000 runes）。
		const size, overlap = 800, 100
		content := strings.Repeat("a", 1000)
		fetcher := &mockArticleFetcher{article: &entity.Article{
			ID:      100,
			Status:  constants.ArticleStatusPublished,
			Content: content,
			Version: 3,
		}}
		// chunkContent(1000 runes, size=800, overlap=100) → step=700 → 2 chunks
		wantChunks := chunkContent(content, size, overlap)
		if len(wantChunks) != 2 {
			t.Fatalf("前置校验：期望 2 chunks，实际 %d", len(wantChunks))
		}
		embed := &mockEmbedder{
			vectors: [][]float32{{0.1, 0.2}, {0.3, 0.4}},
		}
		chunks := &mockChunkWriter{}
		h := &VectorizeHandler{
			articles: fetcher, chunks: chunks, embed: embed,
			cfg: &mockConfigProvider{cfg: &wikiservice.RAGSearchConfig{ChunkSize: size, ChunkOverlap: overlap}},
		}

		err := h.HandleVectorize(context.Background(), makeTask("100"))
		if err != nil {
			t.Fatalf("期望 nil error，实际 %v", err)
		}
		// 验证 embed 被调用，且传入的文本数与 chunk 数一致。
		if !embed.called {
			t.Error("期望 embed 被调用")
		}
		if embed.lastN != len(wantChunks) {
			t.Errorf("期望 embed 收到 %d 文本，实际 %d", len(wantChunks), embed.lastN)
		}
		// 验证先失效旧切片。
		if chunks.deactivateCall != 1 {
			t.Errorf("期望 DeactivateByArticle 调用 1 次，实际 %d", chunks.deactivateCall)
		}
		if chunks.deactivatedID != 100 {
			t.Errorf("期望 DeactivateByArticle 传入 articleID=100，实际 %d", chunks.deactivatedID)
		}
		// 验证写入了正确数量的 chunk。
		if len(chunks.createdChunks) != len(wantChunks) {
			t.Fatalf("期望创建 %d 个 chunk，实际 %d", len(wantChunks), len(chunks.createdChunks))
		}
		// 验证每个 chunk 的字段。
		for i, c := range chunks.createdChunks {
			if c.ArticleID != 100 {
				t.Errorf("chunk[%d].ArticleID = %d，期望 100", i, c.ArticleID)
			}
			if c.ChunkIndex != i {
				t.Errorf("chunk[%d].ChunkIndex = %d，期望 %d", i, c.ChunkIndex, i)
			}
			if c.Content != wantChunks[i] {
				t.Errorf("chunk[%d].Content 长度 %d，期望长度 %d", i, len(c.Content), len(wantChunks[i]))
			}
			if !c.IsActive {
				t.Errorf("chunk[%d].IsActive = false，期望 true", i)
			}
			if c.Version != 3 {
				t.Errorf("chunk[%d].Version = %d，期望 3", i, c.Version)
			}
		}
	})

	t.Run("单chunk成功_完整流程", func(t *testing.T) {
		// 短内容（< chunkSize）→ 单 chunk，覆盖最简路径。
		fetcher := &mockArticleFetcher{article: &entity.Article{
			ID:      1,
			Status:  constants.ArticleStatusPublished,
			Content: "短内容",
			Version: 1,
		}}
		embed := &mockEmbedder{vectors: [][]float32{{0.5}}}
		chunks := &mockChunkWriter{}
		h := &VectorizeHandler{articles: fetcher, chunks: chunks, embed: embed}

		err := h.HandleVectorize(context.Background(), makeTask("1"))
		if err != nil {
			t.Fatalf("期望 nil error，实际 %v", err)
		}
		if len(chunks.createdChunks) != 1 {
			t.Fatalf("期望 1 个 chunk，实际 %d", len(chunks.createdChunks))
		}
		if chunks.createdChunks[0].Content != "短内容" {
			t.Errorf("chunk 内容 %q，期望 %q", chunks.createdChunks[0].Content, "短内容")
		}
	})

	t.Run("HTML正文_切片不含标签与图片URL", func(t *testing.T) {
		// 正文为 TipTap 产出的 HTML：标签/属性/图片 URL 都不得作为语义内容入库。
		fetcher := &mockArticleFetcher{article: &entity.Article{
			ID:      12,
			Status:  constants.ArticleStatusPublished,
			Content: `<h2>用药提示</h2><p>饭后服用</p><img src="https://cdn.example.com/a.png" alt="图">`,
			Version: 1,
		}}
		embed := &mockEmbedder{vectors: [][]float32{{0.5}}}
		chunks := &mockChunkWriter{}
		h := &VectorizeHandler{articles: fetcher, chunks: chunks, embed: embed}

		err := h.HandleVectorize(context.Background(), makeTask("12"))
		if err != nil {
			t.Fatalf("期望 nil error，实际 %v", err)
		}
		if len(chunks.createdChunks) != 1 {
			t.Fatalf("期望 1 个 chunk，实际 %d", len(chunks.createdChunks))
		}
		got := chunks.createdChunks[0].Content
		if got != "用药提示 饭后服用" {
			t.Errorf("chunk 内容 %q，期望 %q", got, "用药提示 饭后服用")
		}
		if strings.ContainsAny(got, "<>") {
			t.Errorf("chunk 内容仍含 HTML 标签：%q", got)
		}
		if strings.Contains(got, "cdn.example.com") {
			t.Errorf("chunk 内容仍含图片 URL：%q", got)
		}
	})

	t.Run("仅图片正文_清除旧切片不报错", func(t *testing.T) {
		fetcher := &mockArticleFetcher{article: &entity.Article{
			ID:      13,
			Status:  constants.ArticleStatusPublished,
			Content: `<p><img src="https://cdn.example.com/a.png"></p>`,
			Version: 1,
		}}
		embed := &mockEmbedder{}
		chunks := &mockChunkWriter{}
		h := &VectorizeHandler{articles: fetcher, chunks: chunks, embed: embed}

		err := h.HandleVectorize(context.Background(), makeTask("13"))
		if err != nil {
			t.Fatalf("期望 nil error（无文本可切片时跳过），实际 %v", err)
		}
		if embed.called {
			t.Error("期望 embed 未被调用（正文只剩图片，无文本）")
		}
		if len(chunks.createdChunks) != 0 {
			t.Errorf("期望 0 个 chunk，实际 %d", len(chunks.createdChunks))
		}
		// P1：纯图片新版是有效重建结果——必须清除旧切片，否则旧指引持续被检索命中。
		if chunks.deactivateCall != 1 || chunks.deactivatedID != 13 {
			t.Errorf("期望清除文章 13 的旧切片，实际 deactivateCall=%d id=%d",
				chunks.deactivateCall, chunks.deactivatedID)
		}
		if chunks.deleteInactiveCall != 1 {
			t.Errorf("期望物理删除失效切片，实际 %d", chunks.deleteInactiveCall)
		}
	})

	t.Run("空内容_清除旧切片不报错", func(t *testing.T) {
		fetcher := &mockArticleFetcher{article: &entity.Article{
			ID:      2,
			Status:  constants.ArticleStatusPublished,
			Content: "",
			Version: 1,
		}}
		embed := &mockEmbedder{}
		chunks := &mockChunkWriter{}
		h := &VectorizeHandler{articles: fetcher, chunks: chunks, embed: embed}

		err := h.HandleVectorize(context.Background(), makeTask("2"))
		if err != nil {
			t.Fatalf("期望 nil error（空内容跳过），实际 %v", err)
		}
		if embed.called {
			t.Error("期望 embed 未被调用（空内容）")
		}
		// P1：空正文版本必须清除旧切片（否则旧内容残留）。
		if chunks.deactivateCall != 1 || chunks.deactivatedID != 2 {
			t.Errorf("期望清除文章 2 的旧切片，实际 deactivateCall=%d id=%d",
				chunks.deactivateCall, chunks.deactivatedID)
		}
		if len(chunks.createdChunks) != 0 {
			t.Errorf("期望 0 个 chunk，实际 %d", len(chunks.createdChunks))
		}
	})

	t.Run("纯空白内容_清除旧切片不报错", func(t *testing.T) {
		// content 为纯空白字符（空格/换行/制表符）时，也应清除旧切片：不调用 embed、不创建 chunk。
		fetcher := &mockArticleFetcher{article: &entity.Article{
			ID:      11,
			Status:  constants.ArticleStatusPublished,
			Content: "   \n\t  ",
			Version: 1,
		}}
		embed := &mockEmbedder{}
		chunks := &mockChunkWriter{}
		h := &VectorizeHandler{articles: fetcher, chunks: chunks, embed: embed}

		err := h.HandleVectorize(context.Background(), makeTask("11"))
		if err != nil {
			t.Fatalf("期望 nil error（纯空白内容跳过），实际 %v", err)
		}
		if embed.called {
			t.Error("期望 embed 未被调用（纯空白内容）")
		}
		if chunks.deactivateCall != 1 || chunks.deactivatedID != 11 {
			t.Errorf("期望清除文章 11 的旧切片，实际 deactivateCall=%d id=%d",
				chunks.deactivateCall, chunks.deactivatedID)
		}
		if len(chunks.createdChunks) != 0 {
			t.Errorf("期望 0 个 chunk（纯空白内容），实际 %d", len(chunks.createdChunks))
		}
	})

	t.Run("embedding失败_返回可重试错误", func(t *testing.T) {
		fetcher := &mockArticleFetcher{article: &entity.Article{
			ID:      5,
			Status:  constants.ArticleStatusPublished,
			Content: "some content",
			Version: 1,
		}}
		embedErr := errors.New("embedding service unavailable")
		embed := &mockEmbedder{err: embedErr}
		chunks := &mockChunkWriter{}
		h := &VectorizeHandler{articles: fetcher, chunks: chunks, embed: embed}

		err := h.HandleVectorize(context.Background(), makeTask("5"))
		if err == nil {
			t.Fatal("期望 error 非 nil")
		}
		// embedding 失败应触发 asynq 重试，不应 SkipRetry。
		if errors.Is(err, asynqlib.SkipRetry) {
			t.Errorf("期望错误不含 SkipRetry（应可重试），实际 %v", err)
		}
		if !errors.Is(err, embedErr) {
			t.Errorf("期望错误包装原始 embedErr，实际 %v", err)
		}
		// embedding 失败不应写 chunk。
		if chunks.deactivateCall != 0 {
			t.Error("期望 DeactivateByArticle 未被调用（embedding 失败）")
		}
		if len(chunks.createdChunks) != 0 {
			t.Errorf("期望 0 个 chunk（embedding 失败），实际 %d", len(chunks.createdChunks))
		}
	})

	t.Run("embedding数量不匹配_返回可重试错误", func(t *testing.T) {
		// 产生 2 chunks，但 embed 仅返回 1 个向量 → 数量不匹配。
		content := strings.Repeat("a", 1000)
		fetcher := &mockArticleFetcher{article: &entity.Article{
			ID:      6,
			Status:  constants.ArticleStatusPublished,
			Content: content,
			Version: 1,
		}}
		embed := &mockEmbedder{vectors: [][]float32{{0.1}}} // 仅 1 个向量，不足
		chunks := &mockChunkWriter{}
		h := &VectorizeHandler{
			articles: fetcher, chunks: chunks, embed: embed,
			cfg: &mockConfigProvider{cfg: &wikiservice.RAGSearchConfig{ChunkSize: 800, ChunkOverlap: 100}},
		}

		err := h.HandleVectorize(context.Background(), makeTask("6"))
		if err == nil {
			t.Fatal("期望 error 非 nil")
		}
		if errors.Is(err, asynqlib.SkipRetry) {
			t.Errorf("期望错误不含 SkipRetry（向量数量不匹配应可重试），实际 %v", err)
		}
		if chunks.deactivateCall != 0 {
			t.Error("期望 DeactivateByArticle 未被调用（数量不匹配）")
		}
	})

	t.Run("DeactivateByArticle失败_返回可重试错误", func(t *testing.T) {
		content := strings.Repeat("a", 1000)
		fetcher := &mockArticleFetcher{article: &entity.Article{
			ID:      9,
			Status:  constants.ArticleStatusPublished,
			Content: content,
			Version: 1,
		}}
		deactivateErr := errors.New("deactivate db error")
		embed := &mockEmbedder{vectors: [][]float32{{0.1}, {0.2}}}
		chunks := &mockChunkWriter{deactivateErr: deactivateErr}
		h := &VectorizeHandler{
			articles: fetcher, chunks: chunks, embed: embed,
			cfg: &mockConfigProvider{cfg: &wikiservice.RAGSearchConfig{ChunkSize: 800, ChunkOverlap: 100}},
		}

		err := h.HandleVectorize(context.Background(), makeTask("9"))
		if err == nil {
			t.Fatal("期望 error 非 nil")
		}
		if errors.Is(err, asynqlib.SkipRetry) {
			t.Errorf("期望错误不含 SkipRetry（DB 写入失败应可重试），实际 %v", err)
		}
		if !errors.Is(err, deactivateErr) {
			t.Errorf("期望错误包装 deactivateErr，实际 %v", err)
		}
		if len(chunks.createdChunks) != 0 {
			t.Errorf("期望 0 个 chunk（deactivate 失败不应写），实际 %d", len(chunks.createdChunks))
		}
	})

	t.Run("embedding期间文章版本变更_放弃写入旧切片", func(t *testing.T) {
		// embedding 期间文章被再次更新（version 1 → 2）：本任务必须放弃写入，
		// 否则会把新版本切片失效并删除（P0 向量版本一致性）。
		fetcher := &mockArticleFetcher{
			article: &entity.Article{
				ID: 20, Status: constants.ArticleStatusPublished, Content: "旧内容", Version: 1,
			},
			lockVersion: 2,
		}
		embed := &mockEmbedder{vectors: [][]float32{{0.1}}}
		chunks := &mockChunkWriter{}
		h := &VectorizeHandler{articles: fetcher, chunks: chunks, embed: embed}

		err := h.HandleVectorize(context.Background(), makeTask("20"))
		if err == nil {
			t.Fatal("期望 error 非 nil（版本已变化应放弃本次写入）")
		}
		if !errors.Is(err, asynqlib.SkipRetry) {
			t.Errorf("期望错误含 SkipRetry（新版本自有其任务，旧任务不再重试），实际 %v", err)
		}
		if !fetcher.lockCalled {
			t.Error("期望 LockVersion 被调用（写入前版本复核）")
		}
		if chunks.deactivateCall != 0 {
			t.Error("版本不一致时不得失效切片")
		}
		if len(chunks.createdChunks) != 0 {
			t.Errorf("版本不一致时不得写入切片，实际写入 %d", len(chunks.createdChunks))
		}
	})

	t.Run("版本复核失败_返回可重试错误且不写切片", func(t *testing.T) {
		lockErr := errors.New("lock db error")
		fetcher := &mockArticleFetcher{
			article: &entity.Article{
				ID: 21, Status: constants.ArticleStatusPublished, Content: "内容", Version: 1,
			},
			lockErr: lockErr,
		}
		chunks := &mockChunkWriter{}
		h := &VectorizeHandler{
			articles: fetcher, chunks: chunks, embed: &mockEmbedder{vectors: [][]float32{{0.1}}},
		}

		err := h.HandleVectorize(context.Background(), makeTask("21"))
		if err == nil {
			t.Fatal("期望 error 非 nil")
		}
		if errors.Is(err, asynqlib.SkipRetry) {
			t.Errorf("期望错误不含 SkipRetry（DB 错误应可重试），实际 %v", err)
		}
		if !errors.Is(err, lockErr) {
			t.Errorf("期望错误包装 lockErr，实际 %v", err)
		}
		if chunks.deactivateCall != 0 || len(chunks.createdChunks) != 0 {
			t.Error("版本复核失败时不得写入切片")
		}
	})

	t.Run("注入事务管理器_切片替换在事务内执行", func(t *testing.T) {
		fetcher := &mockArticleFetcher{article: &entity.Article{
			ID: 22, Status: constants.ArticleStatusPublished, Content: "内容", Version: 1,
		}}
		chunks := &mockChunkWriter{}
		tx := &mockTxRunner{}
		h := &VectorizeHandler{
			articles: fetcher, chunks: chunks, embed: &mockEmbedder{vectors: [][]float32{{0.1}}}, tx: tx,
		}

		if err := h.HandleVectorize(context.Background(), makeTask("22")); err != nil {
			t.Fatalf("期望 nil error，实际 %v", err)
		}
		if !tx.called {
			t.Error("期望 WithTx 被调用（切片原子替换）")
		}
		if len(chunks.createdChunks) != 1 {
			t.Fatalf("期望 1 个切片，实际 %d", len(chunks.createdChunks))
		}
	})

	t.Run("Embedder暴露模型名_切片记录模型版本", func(t *testing.T) {
		fetcher := &mockArticleFetcher{article: &entity.Article{
			ID: 23, Status: constants.ArticleStatusPublished, Content: "内容", Version: 1,
		}}
		chunks := &mockChunkWriter{}
		h := &VectorizeHandler{
			articles: fetcher, chunks: chunks,
			embed: &mockModelEmbedder{
				mockEmbedder: mockEmbedder{vectors: [][]float32{{0.1}}},
				model:        "text-embedding-3-large",
			},
		}

		if err := h.HandleVectorize(context.Background(), makeTask("23")); err != nil {
			t.Fatalf("期望 nil error，实际 %v", err)
		}
		if len(chunks.createdChunks) != 1 {
			t.Fatalf("期望 1 个切片，实际 %d", len(chunks.createdChunks))
		}
		if got := chunks.createdChunks[0].EmbeddingModel; got != "text-embedding-3-large" {
			t.Errorf("切片 EmbeddingModel = %q，期望 %q", got, "text-embedding-3-large")
		}
	})

	t.Run("Embedding热切换_向量模型标识取任务开始快照", func(t *testing.T) {
		// P1：模型 A 生成向量期间切换到模型 B。切片必须标记为 A（生成向量的模型），
		// 否则 A 的向量被标成 B，检索空间混用且自动重建无法识别。
		fetcher := &mockArticleFetcher{article: &entity.Article{
			ID: 24, Status: constants.ArticleStatusPublished, Content: "内容", Version: 1,
		}}
		chunks := &mockChunkWriter{}
		embed := &switchingModelEmbedder{
			mockEmbedder: mockEmbedder{vectors: [][]float32{{0.1}}},
			model:        "model-A",
			onEmbed:      "model-B", // Embed 调用即发生热切换
		}
		h := &VectorizeHandler{articles: fetcher, chunks: chunks, embed: embed}

		if err := h.HandleVectorize(context.Background(), makeTask("24")); err != nil {
			t.Fatalf("期望 nil error，实际 %v", err)
		}
		if len(chunks.createdChunks) != 1 {
			t.Fatalf("期望 1 个切片，实际 %d", len(chunks.createdChunks))
		}
		if got := chunks.createdChunks[0].EmbeddingModel; got != "model-A" {
			t.Errorf("切片 EmbeddingModel = %q，期望 %q（任务开始快照，不得用提交时的当前模型）",
				got, "model-A")
		}
	})

	t.Run("Create失败_返回可重试错误", func(t *testing.T) {
		content := strings.Repeat("a", 1000) // 2 chunks
		fetcher := &mockArticleFetcher{article: &entity.Article{
			ID:      10,
			Status:  constants.ArticleStatusPublished,
			Content: content,
			Version: 1,
		}}
		createErr := errors.New("create chunk db error")
		embed := &mockEmbedder{vectors: [][]float32{{0.1}, {0.2}}}
		chunks := &mockChunkWriter{createErr: createErr}
		h := &VectorizeHandler{
			articles: fetcher, chunks: chunks, embed: embed,
			cfg: &mockConfigProvider{cfg: &wikiservice.RAGSearchConfig{ChunkSize: 800, ChunkOverlap: 100}},
		}

		err := h.HandleVectorize(context.Background(), makeTask("10"))
		if err == nil {
			t.Fatal("期望 error 非 nil")
		}
		if errors.Is(err, asynqlib.SkipRetry) {
			t.Errorf("期望错误不含 SkipRetry（Create 失败应可重试），实际 %v", err)
		}
		if !errors.Is(err, createErr) {
			t.Errorf("期望错误包装 createErr，实际 %v", err)
		}
		// 第一个 chunk 的 Create 被调用（并失败），所以 createdChunks 至少有 1 条记录
		if len(chunks.createdChunks) != 1 {
			t.Errorf("期望 1 个 chunk 被尝试创建，实际 %d", len(chunks.createdChunks))
		}
	})
}

// ============================================================================
// 链条测试：RAG 配置 chunk_size/overlap → HandleVectorize → 生成的切片大小
// 验证管理员通过 PUT /api/staff/config/rag 调整 chunk_size 后，worker 切片行为随之变化。
// ============================================================================

func TestHandleVectorize_RespectsRAGConfigChunkSize(t *testing.T) {
	// 700 rune 内容，chunk_size=300/overlap=30 → step=270 → 3 chunks；每段 ≤300。
	content := strings.Repeat("a", 700)
	fetcher := &mockArticleFetcher{article: &entity.Article{
		ID:      1,
		Status:  constants.ArticleStatusPublished,
		Content: content,
		Version: 1,
	}}
	embed := &mockEmbedder{vectors: [][]float32{{0.1}, {0.2}, {0.3}}}
	chunks := &mockChunkWriter{}
	cfgProv := &mockConfigProvider{cfg: &wikiservice.RAGSearchConfig{
		ChunkSize:    300,
		ChunkOverlap: 30,
	}}
	h := &VectorizeHandler{articles: fetcher, chunks: chunks, embed: embed, cfg: cfgProv}

	if err := h.HandleVectorize(context.Background(), makeTask("1")); err != nil {
		t.Fatalf("期望 nil error，实际 %v", err)
	}
	wantChunks := chunkContent(content, 300, 30)
	if len(wantChunks) != 3 {
		t.Fatalf("前置校验：期望 3 chunks（size=300/overlap=30/700rune），实际 %d", len(wantChunks))
	}
	if len(chunks.createdChunks) != 3 {
		t.Fatalf("期望 3 chunks（chunk_size=300），实际 %d", len(chunks.createdChunks))
	}
	for i, c := range chunks.createdChunks {
		if rc := len([]rune(c.Content)); rc > 300 {
			t.Errorf("chunk[%d] 长度 %d 超过配置 chunk_size=300", i, rc)
		}
	}
}

func TestHandleVectorize_ChunkSizeChangeAltersChunkCount(t *testing.T) {
	// 同一 700 rune 内容：chunk_size=800 → 1 chunk；chunk_size=300 → 3 chunks。
	// 证明配置真的生效，而非硬编码。
	content := strings.Repeat("a", 700)
	run := func(size, overlap int) int {
		fetcher := &mockArticleFetcher{article: &entity.Article{
			ID: 1, Status: constants.ArticleStatusPublished, Content: content, Version: 1,
		}}
		// 预生成足够向量（按 size 估算上限）。
		est := len(chunkContent(content, size, overlap))
		vecs := make([][]float32, est)
		for i := range vecs {
			vecs[i] = []float32{0.1}
		}
		chunks := &mockChunkWriter{}
		h := &VectorizeHandler{
			articles: fetcher, chunks: chunks, embed: &mockEmbedder{vectors: vecs},
			cfg: &mockConfigProvider{cfg: &wikiservice.RAGSearchConfig{ChunkSize: size, ChunkOverlap: overlap}},
		}
		if err := h.HandleVectorize(context.Background(), makeTask("1")); err != nil {
			t.Fatalf("size=%d: %v", size, err)
		}
		return len(chunks.createdChunks)
	}
	if got := run(800, 100); got != 1 {
		t.Errorf("chunk_size=800: 期望 1 chunk，实际 %d", got)
	}
	if got := run(300, 30); got != 3 {
		t.Errorf("chunk_size=300: 期望 3 chunks，实际 %d", got)
	}
}

func TestHandleVectorize_NilConfigProvider_UsesDefaults(t *testing.T) {
	// cfg 为 nil（未注入）时，应回退到 constants.DefaultChunkSize/Overlap（500/50）。
	content := strings.Repeat("a", 700) // 500/50 → step=450 → 2 chunks
	fetcher := &mockArticleFetcher{article: &entity.Article{
		ID: 1, Status: constants.ArticleStatusPublished, Content: content, Version: 1,
	}}
	wantChunks := chunkContent(content, constants.DefaultChunkSize, constants.DefaultChunkOverlap)
	embed := &mockEmbedder{vectors: make([][]float32, len(wantChunks))}
	chunks := &mockChunkWriter{}
	h := &VectorizeHandler{articles: fetcher, chunks: chunks, embed: embed} // cfg 未设置

	if err := h.HandleVectorize(context.Background(), makeTask("1")); err != nil {
		t.Fatalf("期望 nil error，实际 %v", err)
	}
	if len(chunks.createdChunks) != len(wantChunks) {
		t.Fatalf("期望 %d chunks（默认 500/50），实际 %d", len(wantChunks), len(chunks.createdChunks))
	}
}

// ============================================================================
// 链条测试：ArticleService.Update → Enqueue → VectorizeHandler → 写新切片
// 验证已发布文章内容变更后的完整链路（跨 service/adapter 两层）：
//
//	Phase 1: Update 检测 content_hash 变化 → 事务内 DeactivateByArticle → 事务后 Enqueue
//	Phase 2: VectorizeHandler 消费任务 → 读 RAG 配置 → 切片 → embedding → 写新切片
//
// ============================================================================

// chainArticleRepo 实现 wikiservice.ArticleRepoPort；UpdateFields 模拟 DB 更新（应用 content 变更并递增版本）。
type chainArticleRepo struct {
	article *entity.Article
}

func (m *chainArticleRepo) Create(_ context.Context, _ *entity.Article) error { return nil }
func (m *chainArticleRepo) GetByID(_ context.Context, _ int64) (*entity.Article, error) {
	return m.article, nil
}
func (m *chainArticleRepo) GetPublishedByID(_ context.Context, _ int64) (*entity.Article, error) {
	return m.article, nil
}
func (m *chainArticleRepo) ListPublished(_ context.Context, _ repository.ListPublishedFilter, _, _ int) ([]*entity.Article, int64, error) {
	return nil, 0, nil
}
func (m *chainArticleRepo) ListFeatured(_ context.Context, _ *int64, _ int) ([]*entity.Article, error) {
	return nil, nil
}
func (m *chainArticleRepo) SetFeaturedRank(_ context.Context, _ int64, _ int) error { return nil }
func (m *chainArticleRepo) ListForStaff(_ context.Context, _ repository.ListStaffFilter, _, _ int) ([]*entity.Article, int64, error) {
	return nil, 0, nil
}
func (m *chainArticleRepo) UpdateFields(_ context.Context, _ int64, fields repository.UpdateFields) (*entity.Article, error) {
	updated := *m.article
	if fields.Content != nil {
		updated.Content = *fields.Content
	}
	if fields.ContentHash != nil {
		updated.ContentHash = *fields.ContentHash
	}
	// 与真实 SQL 一致：内容变更时按行当前状态判定（published → pending + 版本递增，P1）。
	if fields.ReReviewOnContentChange && updated.Status == constants.ArticleStatusPublished {
		updated.Status = constants.ArticleStatusPending
		updated.Version++
	} else if fields.IncrementVersion {
		updated.Version++
	}
	return &updated, nil
}
func (m *chainArticleRepo) UpdateStatus(_ context.Context, _ int64, _, _ string, _ repository.StatusUpdateOpts) error {
	return nil
}
func (m *chainArticleRepo) SoftDelete(_ context.Context, _ int64) error { return nil }

// chainAuditRepo 实现 wikiservice.AuditRepoPort。
type chainAuditRepo struct{ cnt int }

func (m *chainAuditRepo) Create(_ context.Context, _ *entity.ArticleAuditLog) error {
	m.cnt++
	return nil
}

// chainChunkRepo 实现 wikiservice.ChunkRepoPort，记录 DeactivateByArticle 调用。
type chainChunkRepo struct {
	deactivateCall int
	deactivateID   int64
}

func (m *chainChunkRepo) DeactivateByArticle(_ context.Context, articleID int64) (int64, error) {
	m.deactivateCall++
	m.deactivateID = articleID
	return 0, nil
}
func (m *chainChunkRepo) ListActiveByArticle(_ context.Context, _ int64) ([]*entity.ArticleChunk, error) {
	return nil, nil
}

// chainTxRunner 实现 wikiservice.TxRunner，直接同步执行 fn（不开真实事务）。
type chainTxRunner struct{ called bool }

func (m *chainTxRunner) WithTx(_ context.Context, fn func(ctx context.Context) error) error {
	m.called = true
	return fn(context.Background())
}

// chainEnqueuer 实现 wikiservice.VectorizeEnqueuer，捕获入队的 articleID。
type chainEnqueuer struct {
	enqueuedIDs []int64
}

func (m *chainEnqueuer) Enqueue(_ context.Context, articleID int64) error {
	m.enqueuedIDs = append(m.enqueuedIDs, articleID)
	return nil
}

func TestChain_UpdateFlow(t *testing.T) {
	deptID := int64(10)
	oldContent := "旧内容"
	newContent := strings.Repeat("新", 700) // 700 rune，chunk_size=300/overlap=30 → 3 chunks

	article := &entity.Article{
		ID:           42,
		Status:       constants.ArticleStatusPublished,
		Content:      oldContent,
		ContentHash:  contenthash.SHA256(oldContent),
		Version:      1,
		AuthorID:     1,
		DepartmentID: &deptID,
	}

	// ---- Phase 1: ArticleService.Update ----
	repo := &chainArticleRepo{article: article}
	audit := &chainAuditRepo{}
	svcChunks := &chainChunkRepo{}
	tx := &chainTxRunner{}
	enqueuer := &chainEnqueuer{}

	svc := wikiservice.NewArticleService(repo, audit, svcChunks, tx, enqueuer, nil, nil)

	_, err := svc.Update(context.Background(), wikiservice.UpdateInput{
		ArticleID: 42,
		Content:   &newContent,
		Actor:     wikiservice.Actor{UserID: 1, Role: constants.RoleDoctor, DeptID: 10},
	})
	if err != nil {
		t.Fatalf("Phase1 Update 失败: %v", err)
	}

	if !tx.called {
		t.Error("期望 WithTx 被调用")
	}
	// P1：已发布文章内容变更 → 回到待审核。审核通过前继续服务上一次审核通过的切片：
	// 不得失效旧切片、不得重新向量化（否则未审核内容会进入患者可见的知识库）。
	if svcChunks.deactivateCall != 0 {
		t.Errorf("重新审核期间不得失效原切片，实际调用 %d 次", svcChunks.deactivateCall)
	}
	if len(enqueuer.enqueuedIDs) != 0 {
		t.Errorf("重新审核期间不得入队向量化，实际 %v", enqueuer.enqueuedIDs)
	}
	if audit.cnt != 1 {
		t.Errorf("期望审计写入 1 条，实际 %d", audit.cnt)
	}

	// ---- Phase 2: VectorizeHandler 消费任务 ----
	updatedArticle := &entity.Article{
		ID:      42,
		Status:  constants.ArticleStatusPublished,
		Content: newContent,
		Version: 2,
	}

	const chunkSize, chunkOverlap = 300, 30
	wantChunks := chunkContent(newContent, chunkSize, chunkOverlap)
	if len(wantChunks) != 3 {
		t.Fatalf("前置校验：期望 3 chunks（700rune/size=300/overlap=30），实际 %d", len(wantChunks))
	}

	vecs := make([][]float32, len(wantChunks))
	for i := range vecs {
		vecs[i] = []float32{float32(i) * 0.1}
	}

	fetcher := &mockArticleFetcher{article: updatedArticle}
	embed := &mockEmbedder{vectors: vecs}
	handlerChunks := &mockChunkWriter{}
	cfgProv := &mockConfigProvider{cfg: &wikiservice.RAGSearchConfig{
		ChunkSize:    chunkSize,
		ChunkOverlap: chunkOverlap,
	}}

	h := &VectorizeHandler{articles: fetcher, chunks: handlerChunks, embed: embed, cfg: cfgProv}

	// Phase 1 不入队（内容变更回到待审核）。此处手动入队以单独验证 Worker 消费链路
	// （真实路径下由管理员审核通过 Approve 入队）。
	if err := enqueuer.Enqueue(context.Background(), 42); err != nil {
		t.Fatalf("Phase2 入队失败: %v", err)
	}
	task := makeTask(strconv.FormatInt(enqueuer.enqueuedIDs[0], 10))
	if err := h.HandleVectorize(context.Background(), task); err != nil {
		t.Fatalf("Phase2 HandleVectorize 失败: %v", err)
	}

	if handlerChunks.deactivateCall != 1 {
		t.Errorf("期望 VectorizeHandler DeactivateByArticle 调用 1 次（幂等失效），实际 %d", handlerChunks.deactivateCall)
	}
	if len(handlerChunks.createdChunks) != len(wantChunks) {
		t.Fatalf("期望写入 %d 个新切片，实际 %d", len(wantChunks), len(handlerChunks.createdChunks))
	}
	for i, c := range handlerChunks.createdChunks {
		if c.ArticleID != 42 {
			t.Errorf("chunk[%d].ArticleID = %d，期望 42", i, c.ArticleID)
		}
		if c.Content != wantChunks[i] {
			t.Errorf("chunk[%d].Content 不匹配（长度 %d vs %d）", i, len([]rune(c.Content)), len([]rune(wantChunks[i])))
		}
		if c.Version != 2 {
			t.Errorf("chunk[%d].Version = %d，期望 2（版本递增）", i, c.Version)
		}
		if !c.IsActive {
			t.Errorf("chunk[%d].IsActive = false，期望 true", i)
		}
	}
}
