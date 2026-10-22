// ChatSendService 纯函数单元测试（REQ-CHAT-006-A Token 预算 + 转换辅助）。
// 不依赖 DB / LLM / Redis，覆盖 estimateTokens / estimateHistoryTokens /
// trimHistoryForTokens / truncateTitle / deptIDPtr / toEntityRefs / toLLMMessages。
package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"health-nexus/internal/domain/chat/entity"
	"health-nexus/internal/platform/llm"
	"health-nexus/internal/shared/constants"
	apperrors "health-nexus/internal/shared/errors"
	"health-nexus/internal/shared/rag"
)

// ============================================================================
// estimateTokens：中文 1 char ≈ 1.5 token，英文 1 char ≈ 0.25 token，数字/符号 0.5
// ============================================================================

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int
	}{
		// 中文：每字符 1.5 token（实际均值 1-2）
		{"中文-两字", "你好", 3},        // 2 × 1.5 = 3.0
		{"中文-四字", "你好世界", 6},      // 4 × 1.5 = 6.0
		{"中文-单字（向上取整）", "中", 2},   // 1 × 1.5 = 1.5 → ceil = 2
		{"中文-长句", "我是一个测试语句", 12}, // 8 × 1.5 = 12.0
		// 英文：每字符 0.25 token（4 字符≈1 token）
		{"英文-两字符", "hi", 1},          // 2 × 0.25 = 0.5 → ceil = 1
		{"英文-四字符", "test", 1},        // 4 × 0.25 = 1.0
		{"英文-长短语", "hello world", 3}, // 10 字母 × 0.25 + 1 空格 = 2.5 → ceil = 3
		// 中英文混合
		{"混合", "hi你好", 4},                // 2 × 0.25 + 2 × 1.5 = 0.5 + 3.0 = 3.5 → 4
		{"混合-中文英文数字", "你好 world 123", 6}, // 2 × 1.5 + 5 × 0.25 + 3 × 0.5 = 3 + 1.25 + 1.5 = 5.75 → 6
		// 边界
		{"空字符串", "", 0},
		// 长文本（验证线性）— '\x00' 字符走 default 分支 0.5 token
		{"长文本-100字符", string(make([]rune, 100)), 50},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// 用 rune 切片构造的 "长文本-100字符" 默认是 '\x00'，长度仍为 100 rune
			got := estimateTokens(tc.text)
			if got != tc.want {
				t.Errorf("estimateTokens(%q) = %d, want %d", tc.text, got, tc.want)
			}
		})
	}
}

// ============================================================================
// estimateHistoryTokens：当前查询 + 历史累加
// ============================================================================

func TestEstimateHistoryTokens(t *testing.T) {
	t.Run("空历史_仅当前查询", func(t *testing.T) {
		// 当前查询 "test" = 4 × 0.25 = 1 token
		got := estimateHistoryTokens(nil, "test")
		want := 1
		if got != want {
			t.Errorf("got %d, want %d", got, want)
		}
	})

	t.Run("累加历史消息", func(t *testing.T) {
		// 当前查询 "test" = 1 token
		// 历史 2 条："hello" = ceil(5 × 0.25) = 2 + "world" = ceil(5 × 0.25) = 2
		// 总计 5 token
		history := []*entity.Message{
			{Content: "hello"},
			{Content: "world"},
		}
		got := estimateHistoryTokens(history, "test")
		want := 5
		if got != want {
			t.Errorf("got %d, want %d", got, want)
		}
	})

	t.Run("空查询_仅历史", func(t *testing.T) {
		history := []*entity.Message{{Content: "ab"}} // ceil(2 × 0.25) = 1 token
		got := estimateHistoryTokens(history, "")
		want := 1
		if got != want {
			t.Errorf("got %d, want %d", got, want)
		}
	})
}

// ============================================================================
// trimHistoryForTokens：FIFO 丢弃最早轮次
// ============================================================================

// makeHistory 构造 n 轮（2n 条）历史消息，每条 content 由 tokenPerMsg 决定。
// content 用 '\x00' 字符（走 default 分支 0.5 token/字符），故 tokenPerMsg token = tokenPerMsg*2 rune。
func makeHistory(turns, tokenPerMsg int) []*entity.Message {
	out := make([]*entity.Message, 0, turns*2)
	content := string(make([]rune, tokenPerMsg*2))
	for i := 0; i < turns; i++ {
		out = append(out,
			&entity.Message{Role: constants.MessageRoleUser, Content: content},
			&entity.Message{Role: constants.MessageRoleAssistant, Content: content},
		)
	}
	return out
}

func TestTrimHistoryForTokens(t *testing.T) {
	t.Run("空历史_返回空", func(t *testing.T) {
		got := trimHistoryForTokens(nil, "query", 100)
		if len(got) != 0 {
			t.Errorf("期望空切片，实际长度 %d", len(got))
		}
	})

	t.Run("budget充足_不裁剪", func(t *testing.T) {
		// 2 轮历史，每条 1 token = 总 4 token + query "test" 1 token = 5 token
		// budget 100 远大于 5，不裁剪
		history := makeHistory(2, 1)
		got := trimHistoryForTokens(history, "test", 100)
		if len(got) != 4 {
			t.Errorf("期望 4 条，实际 %d", len(got))
		}
	})

	t.Run("超预算_FIFO丢弃最早轮次", func(t *testing.T) {
		// 3 轮历史，每条 10 token = 总 60 token + query "test" 1 token = 61 token
		// budget 41 → 丢一轮（20 token）后剩 41，41 > 41 false，停止
		history := makeHistory(3, 10)
		got := trimHistoryForTokens(history, "test", 41)
		if len(got) != 4 {
			t.Errorf("期望 4 条（丢 1 轮），实际 %d", len(got))
		}
	})

	t.Run("超预算_丢弃多轮直到满足", func(t *testing.T) {
		// 3 轮历史，每条 10 token = 总 60 + query 1 = 61
		// budget 21 → 丢到只剩 1 轮（20 + query 1 = 21），正好不超
		history := makeHistory(3, 10)
		got := trimHistoryForTokens(history, "test", 21)
		if len(got) != 2 {
			t.Errorf("期望 2 条（丢 2 轮），实际 %d", len(got))
		}
	})

	t.Run("单条消息已超budget_清空到小于2", func(t *testing.T) {
		// 1 轮历史，每条 100 token = 总 200 + query 1 = 201
		// budget 50 → 丢一轮后 len=0，停止
		history := makeHistory(1, 100)
		got := trimHistoryForTokens(history, "test", 50)
		if len(got) != 0 {
			t.Errorf("期望 0 条（清空），实际 %d", len(got))
		}
	})

	t.Run("history长度小于2_不裁剪", func(t *testing.T) {
		// 仅 1 条历史，无论 token 多大都不进入循环
		history := []*entity.Message{{Content: string(make([]rune, 1000))}}
		got := trimHistoryForTokens(history, "test", 1)
		if len(got) != 1 {
			t.Errorf("期望 1 条（不裁剪），实际 %d", len(got))
		}
	})

	t.Run("history刚好2条且不超budget_不裁剪", func(t *testing.T) {
		history := makeHistory(1, 1) // 2 条，每条 1 token
		// 总 2 + query 1 = 3，budget 3 → 3 > 3 false，不裁剪
		got := trimHistoryForTokens(history, "test", 3)
		if len(got) != 2 {
			t.Errorf("期望 2 条（边界不裁剪），实际 %d", len(got))
		}
	})

	t.Run("history刚好2条且超budget_清空", func(t *testing.T) {
		history := makeHistory(1, 1) // 2 条，每条 1 token
		// 总 2 + query 1 = 3，budget 2 → 3 > 2 true，丢一轮后 len=0
		got := trimHistoryForTokens(history, "test", 2)
		if len(got) != 0 {
			t.Errorf("期望 0 条（边界裁剪），实际 %d", len(got))
		}
	})
}

// ============================================================================
// truncateTitle：首条消息前 TitleMaxLen 字符截断
// ============================================================================

func TestTruncateTitle(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want string
	}{
		{"短消息_不截断", "高血压怎么控制", "高血压怎么控制"},
		{"刚好20字符_不截断", string(make([]rune, entity.TitleMaxLen)), string(make([]rune, entity.TitleMaxLen))},
		{"超长_截断到20", string(make([]rune, 100)), string(make([]rune, entity.TitleMaxLen))},
		{"空字符串", "", ""},
		{"单字符", "中", "中"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateTitle(tc.msg)
			if got != tc.want {
				t.Errorf("got %q (len=%d), want %q (len=%d)",
					got, len([]rune(got)), tc.want, len([]rune(tc.want)))
			}
			// 长度上限校验
			if gotRune := len([]rune(got)); gotRune > entity.TitleMaxLen {
				t.Errorf("截断后长度 %d 超过 TitleMaxLen=%d", gotRune, entity.TitleMaxLen)
			}
		})
	}
}

// ============================================================================
// deptIDPtr：会话已锁定时优先用锁定值
// ============================================================================

func TestDeptIDPtr(t *testing.T) {
	t.Run("会话已锁定_返回LockedDeptID", func(t *testing.T) {
		locked := int64(99)
		conv := &entity.Conversation{LockedDeptID: &locked}
		got := deptIDPtr(nil, conv)
		if got == nil {
			t.Fatal("期望非 nil")
		}
		if *got != 99 {
			t.Errorf("got %d, want 99（locked 值）", *got)
		}
	})

	t.Run("会话未锁定_返回nil_不限科室", func(t *testing.T) {
		conv := &entity.Conversation{LockedDeptID: nil}
		got := deptIDPtr(nil, conv)
		if got != nil {
			t.Errorf("期望 nil（不限科室检索），实际: %v", got)
		}
	})

	t.Run("显式选择全部科室_返回nil_不限科室", func(t *testing.T) {
		locked := int64(99)
		conv := &entity.Conversation{LockedDeptID: &locked}
		allDept := int64(0)
		got := deptIDPtr(&allDept, conv)
		if got != nil {
			t.Errorf("显式选 0 时应返回 nil，实际: %v", got)
		}
	})

}

// ============================================================================
// toEntityRefs：rag.Chunk → entity.Reference
// ============================================================================

func TestToEntityRefs(t *testing.T) {
	t.Run("空切片", func(t *testing.T) {
		got := toEntityRefs(nil)
		if len(got) != 0 {
			t.Errorf("期望空切片，实际 %d", len(got))
		}
	})

	t.Run("多元素转换", func(t *testing.T) {
		chunks := []rag.Chunk{
			{ChunkID: "c1", ArticleID: "a1", ArticleTitle: "t1", Content: "x", Score: 0.9},
			{ChunkID: "c2", ArticleID: "a2", ArticleTitle: "t2", Content: "y", Score: 0.5},
		}
		got := toEntityRefs(chunks)
		if len(got) != 2 {
			t.Fatalf("期望 2 个，实际 %d", len(got))
		}
		// 逐字段校验
		if got[0].ChunkID != "c1" || got[0].ArticleID != "a1" || got[0].ArticleTitle != "t1" ||
			got[0].Content != "x" || got[0].Score != 0.9 {
			t.Errorf("第一个元素字段不匹配: %+v", got[0])
		}
		if got[1].ChunkID != "c2" || got[1].ArticleID != "a2" || got[1].ArticleTitle != "t2" ||
			got[1].Content != "y" || got[1].Score != 0.5 {
			t.Errorf("第二个元素字段不匹配: %+v", got[1])
		}
	})
}

// ============================================================================
// toLLMMessages：entity.Message → llm.Message
// ============================================================================

func TestToLLMMessages(t *testing.T) {
	t.Run("空切片", func(t *testing.T) {
		got := toLLMMessages(nil)
		if len(got) != 0 {
			t.Errorf("期望空切片，实际 %d", len(got))
		}
	})

	t.Run("保留Role和Content", func(t *testing.T) {
		uid := uuid.New()
		history := []*entity.Message{
			{ID: uid, Role: constants.MessageRoleUser, Content: "你好"},
			{Role: constants.MessageRoleAssistant, Content: "您好"},
		}
		got := toLLMMessages(history)
		if len(got) != 2 {
			t.Fatalf("期望 2 条，实际 %d", len(got))
		}
		if got[0].Role != constants.MessageRoleUser || got[0].Content != "你好" {
			t.Errorf("第一条不匹配: %+v", got[0])
		}
		if got[1].Role != constants.MessageRoleAssistant || got[1].Content != "您好" {
			t.Errorf("第二条不匹配: %+v", got[1])
		}
	})

	t.Run("不携带 entity 额外字段", func(t *testing.T) {
		// llm.Message 只有 Role/Content，不应携带 ID/ReferencedChunks 等
		history := []*entity.Message{
			{Role: "user", Content: "x", ReferencedChunks: []entity.Reference{{ChunkID: "c"}}},
		}
		got := toLLMMessages(history)
		// 编译期校验：llm.Message 仅有 Role/Content 两个字段
		var _ = llm.Message{Role: got[0].Role, Content: got[0].Content}
		if got[0].Role != "user" || got[0].Content != "x" {
			t.Errorf("字段不匹配: %+v", got[0])
		}
	})
}

// ============================================================================
// validateStreamInput：消息长度校验（覆盖 Stream 入口的纯校验函数）
// ============================================================================

func TestValidateStreamInput(t *testing.T) {
	t.Run("空消息_BadRequest", func(t *testing.T) {
		err := validateStreamInput(StreamInput{Message: ""})
		if err == nil {
			t.Fatal("期望 error")
		}
		appErr, ok := err.(*apperrors.AppError)
		if !ok {
			t.Fatalf("期望 *AppError，实际 %T", err)
		}
		if appErr.HTTP != 400 {
			t.Errorf("期望 HTTP=400，实际 %d", appErr.HTTP)
		}
	})

	t.Run("正常长度_通过", func(t *testing.T) {
		err := validateStreamInput(StreamInput{Message: "高血压怎么控制"})
		if err != nil {
			t.Errorf("期望 nil，实际 %v", err)
		}
	})

	t.Run("刚好2000字符_通过", func(t *testing.T) {
		msg := string(make([]rune, constants.MaxMessageLength))
		err := validateStreamInput(StreamInput{Message: msg})
		if err != nil {
			t.Errorf("期望 nil，实际 %v", err)
		}
	})

	t.Run("超长_Validation422", func(t *testing.T) {
		msg := string(make([]rune, constants.MaxMessageLength+1))
		err := validateStreamInput(StreamInput{Message: msg})
		if err == nil {
			t.Fatal("期望 error")
		}
		appErr, ok := err.(*apperrors.AppError)
		if !ok {
			t.Fatalf("期望 *AppError，实际 %T", err)
		}
		if appErr.HTTP != 422 {
			t.Errorf("期望 HTTP=422，实际 %d", appErr.HTTP)
		}
	})
}

// ============================================================================
// chunkContents：提取切片内容，过滤空/纯空白内容
// ============================================================================

func TestChunkContents(t *testing.T) {
	t.Run("空切片", func(t *testing.T) {
		got := chunkContents(nil)
		if len(got) != 0 {
			t.Errorf("期望空切片，实际 %d", len(got))
		}
	})

	t.Run("过滤空字符串和纯空白_仅保留正常内容", func(t *testing.T) {
		chunks := []rag.Chunk{
			{ChunkID: "c1", Content: "高血压的日常管理"},
			{ChunkID: "c2", Content: ""},          // 空字符串
			{ChunkID: "c3", Content: "   \n\t  "}, // 纯空白
			{ChunkID: "c4", Content: "\n\n\n"},    // 纯换行
			{ChunkID: "c5", Content: "   "},       // 纯空格
			{ChunkID: "c6", Content: "低盐低脂饮食"},
		}
		got := chunkContents(chunks)
		want := []string{"高血压的日常管理", "低盐低脂饮食"}
		if len(got) != len(want) {
			t.Fatalf("期望 %d 个内容 %v，实际 %d 个 %v",
				len(want), want, len(got), got)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("content[%d] = %q，期望 %q", i, got[i], want[i])
			}
		}
	})

	t.Run("全部为空或空白_返回空切片", func(t *testing.T) {
		chunks := []rag.Chunk{
			{ChunkID: "c1", Content: ""},
			{ChunkID: "c2", Content: "   "},
			{ChunkID: "c3", Content: "\n\t  "},
		}
		got := chunkContents(chunks)
		if len(got) != 0 {
			t.Errorf("期望 0 个内容（全空白），实际 %d 个 %v", len(got), got)
		}
	})

	t.Run("全部为正常内容_全部保留", func(t *testing.T) {
		chunks := []rag.Chunk{
			{ChunkID: "c1", Content: "内容一"},
			{ChunkID: "c2", Content: "内容二"},
		}
		got := chunkContents(chunks)
		if len(got) != 2 {
			t.Fatalf("期望 2 个内容，实际 %d", len(got))
		}
		if got[0] != "内容一" || got[1] != "内容二" {
			t.Errorf("内容不匹配: %v", got)
		}
	})
}

// ============================================================================
// citedArticleCount：统计答案实际引用的文章数（去重、忽略越界/非法编号）
// ============================================================================

func TestCitedArticleCount(t *testing.T) {
	chunks := []rag.Chunk{
		{ChunkID: "c1", ArticleID: "A"},
		{ChunkID: "c2", ArticleID: "B"},
		{ChunkID: "c3", ArticleID: "A"}, // 同篇文章多切片，应去重
	}
	cases := []struct {
		name, answer string
		want         int
	}{
		{"无引用", "没有任何标记的答案", 0},
		{"单篇多次引用去重", "结论A[资料1]，结论B[资料1]", 1},
		{"多篇", "先[资料1]后[资料2]", 2},
		{"空格容错", "[资料 2]", 1},
		{"越界编号忽略", "[资料9]", 0},
		{"非法编号忽略", "[资料x] 与 [资料1]", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := citedArticleCount(c.answer, chunks); got != c.want {
				t.Errorf("citedArticleCount(%q) = %d, want %d", c.answer, got, c.want)
			}
		})
	}
}

// ============================================================================
// groupChunksByArticle：引用编号以"文章"为单位，与前端参考来源列表（按 article_id 去重）一致
// ============================================================================

func TestGroupChunksByArticle(t *testing.T) {
	t.Run("同篇文章多切片_归并为一条_分数取最大_风险不稀释", func(t *testing.T) {
		chunks := []rag.Chunk{
			{ChunkID: "c1", ArticleID: "1", Content: "切口护理", Score: 0.71, VecScore: 0.71, ContentRisk: "normal"},
			{ChunkID: "c2", ArticleID: "1", Content: "疼痛管理", Score: 0.68, VecScore: 0.68},
			{ChunkID: "c3", ArticleID: "1", Content: "饮食营养", Score: 0.66, VecScore: 0.66, ContentRisk: contentRiskHigh},
		}
		got := groupChunksByArticle(chunks)
		if len(got) != 1 {
			t.Fatalf("期望归并为 1 条，实际 %d", len(got))
		}
		if got[0].ArticleID != "1" {
			t.Errorf("article_id = %q, want 1", got[0].ArticleID)
		}
		if got[0].Score != 0.71 || got[0].VecScore != 0.71 {
			t.Errorf("分数应取最大 0.71，实际 score=%v vec=%v", got[0].Score, got[0].VecScore)
		}
		if got[0].ContentRisk != contentRiskHigh {
			t.Errorf("任一切片 high 时整体应为 high，实际 %q", got[0].ContentRisk)
		}
		for _, part := range []string{"切口护理", "疼痛管理", "饮食营养"} {
			if !strings.Contains(got[0].Content, part) {
				t.Errorf("归并内容缺少 %q：%q", part, got[0].Content)
			}
		}
	})

	t.Run("多篇文章交错_按首次出现排序且不合并", func(t *testing.T) {
		chunks := []rag.Chunk{
			{ChunkID: "a1c1", ArticleID: "A", Content: "A1"},
			{ChunkID: "b1c1", ArticleID: "B", Content: "B1"},
			{ChunkID: "a1c2", ArticleID: "A", Content: "A2"},
		}
		got := groupChunksByArticle(chunks)
		if len(got) != 2 {
			t.Fatalf("期望 2 篇文章，实际 %d", len(got))
		}
		if got[0].ArticleID != "A" || got[1].ArticleID != "B" {
			t.Errorf("应按首次出现排序 [A B]，实际 [%s %s]", got[0].ArticleID, got[1].ArticleID)
		}
		if !strings.Contains(got[0].Content, "A1") || !strings.Contains(got[0].Content, "A2") {
			t.Errorf("文章 A 的切片未合并：%q", got[0].Content)
		}
	})

	t.Run("空内容过滤_无 article_id 独立保留", func(t *testing.T) {
		chunks := []rag.Chunk{
			{ChunkID: "x", ArticleID: "A", Content: "  "},   // 纯空白 → 过滤
			{ChunkID: "y", ArticleID: "", Content: "无归属内容"}, // 无 article_id → 独立保留
			{ChunkID: "z", ArticleID: "A", Content: "有效内容"},
		}
		got := groupChunksByArticle(chunks)
		if len(got) != 2 {
			t.Fatalf("期望 2 条，实际 %d", len(got))
		}
		if got[0].ChunkID != "y" || got[1].ArticleID != "A" {
			t.Errorf("顺序/内容不符: %+v", got)
		}
	})
}

// ============================================================================
// 生成侧 token 预算：提示词 + 切片 + 历史 共享同一预算（REQ-CHAT-006-A）
// ============================================================================

func TestGenerationBudget(t *testing.T) {
	t.Run("扣除提示词与问题", func(t *testing.T) {
		prompt := string(make([]rune, 1000)) // '\x00' → 500 token
		query := "test"                      // 1 token
		got := generationBudget(prompt, query)
		if want := constants.TokenBudgetGenerate - 501; got != want {
			t.Errorf("期望 %d，实际 %d", want, got)
		}
	})

	t.Run("提示词过长时不低于下限", func(t *testing.T) {
		// 40000 rune ≈ 20000 token > 预算 → 必须收敛到下限而不是负数
		got := generationBudget(string(make([]rune, 40000)), "test")
		if got != minGenerationBudget {
			t.Errorf("期望下限 %d，实际 %d", minGenerationBudget, got)
		}
	})
}

func TestTrimChunksForTokens(t *testing.T) {
	// chunk 内容用 '\x00'（0.5 token/rune）：400 rune ≈ 200 token
	mk := func(id string, runes int) rag.Chunk {
		return rag.Chunk{ChunkID: id, Content: string(make([]rune, runes))}
	}

	t.Run("超预算从尾部丢弃低相关切片", func(t *testing.T) {
		chunks := []rag.Chunk{mk("1", 400), mk("2", 400), mk("3", 400)} // 共 600 token
		got := trimChunksForTokens(chunks, 450)
		if len(got) != 2 {
			t.Fatalf("期望保留 2 条，实际 %d", len(got))
		}
		if got[0].ChunkID != "1" || got[1].ChunkID != "2" {
			t.Errorf("应从尾部丢弃，实际保留 %s,%s", got[0].ChunkID, got[1].ChunkID)
		}
	})

	t.Run("至少保留一条（单条超预算也不丢空）", func(t *testing.T) {
		chunks := []rag.Chunk{mk("1", 4000)}
		got := trimChunksForTokens(chunks, 10)
		if len(got) != 1 {
			t.Errorf("切片是答案唯一依据，不得清空，实际 %d 条", len(got))
		}
	})

	t.Run("预算充足不裁剪", func(t *testing.T) {
		chunks := []rag.Chunk{mk("1", 40), mk("2", 40)}
		got := trimChunksForTokens(chunks, 1000)
		if len(got) != 2 {
			t.Errorf("期望 2 条，实际 %d", len(got))
		}
	})
}

// ============================================================================
// 系统提示词分层：可配置人格 + 代码独占护栏
// ============================================================================

// fakePromptProvider 固定返回预设选择（或错误），用于验证分层与降级。
type fakePromptProvider struct {
	sel rag.SystemPromptSelection
	err error
}

func (f fakePromptProvider) SystemPrompt(context.Context) (rag.SystemPromptSelection, error) {
	return f.sel, f.err
}

func TestBuildSystemPrompt(t *testing.T) {
	t.Run("provider为nil_默认人格加护栏", func(t *testing.T) {
		s := &ChatSendService{}
		got, sel := s.buildSystemPrompt(context.Background())
		if !strings.Contains(got, constants.SystemPromptPersona) {
			t.Error("应包含默认人格")
		}
		if !strings.Contains(got, constants.SystemPromptGuardrails) {
			t.Error("必须包含代码护栏")
		}
		if sel.TemplateID != 0 || sel.Version != 0 {
			t.Errorf("无 DB 模板时来源应为零值，实际 %+v", sel)
		}
	})

	t.Run("DB人格_替换默认人格但保留护栏并可溯源", func(t *testing.T) {
		s := &ChatSendService{promptProvider: fakePromptProvider{
			sel: rag.SystemPromptSelection{Persona: "你是中医科宣教助手。", TemplateID: 12, Version: 5},
		}}
		got, sel := s.buildSystemPrompt(context.Background())
		if !strings.HasPrefix(got, "你是中医科宣教助手。") {
			t.Errorf("应以 DB 人格开头，实际: %s", got)
		}
		if strings.Contains(got, constants.SystemPromptPersona) {
			t.Error("DB 人格应替换默认人格")
		}
		if !strings.Contains(got, constants.SystemPromptGuardrails) {
			t.Error("DB 人格不得覆盖代码护栏")
		}
		if sel.TemplateID != 12 || sel.Version != 5 {
			t.Errorf("溯源信息丢失: %+v", sel)
		}
	})

	t.Run("provider故障_降级默认人格而非无护栏", func(t *testing.T) {
		s := &ChatSendService{promptProvider: fakePromptProvider{err: errors.New("config down")}}
		got, sel := s.buildSystemPrompt(context.Background())
		if !strings.Contains(got, constants.SystemPromptGuardrails) {
			t.Error("加载失败也必须保留代码护栏")
		}
		if sel.TemplateID != 0 {
			t.Errorf("降级路径不应声称有 DB 模板: %+v", sel)
		}
	})
}
