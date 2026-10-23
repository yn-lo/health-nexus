package service

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"

	"health-nexus/internal/domain/chat/entity"
	"health-nexus/internal/platform/llm"
	"health-nexus/internal/shared/constants"
	"health-nexus/internal/shared/rag"
)

// estimateTokens 估算文本的 token 数。
// ponytail: 真实 tokenizer 需依赖 tiktoken-go（引入新依赖），折中；
// 当前按字符类别加权近似——中文 1.5 token/字符（实际均值 1-2）、英文 0.25 token/字符（4 字符≈1 token）、
// 数字与符号 0.5 token/字符、空格不计。中文场景比 utf8.RuneCountInString/2 更接近实际，避免超长上下文突破 LLM 窗口。
// 上限——仍是估算，极端文本（混合未登录词、罕见 Unicode）可能偏差；
// 升级路径：阶段 2 接入 tiktoken-go 后改为精确估算。
func estimateTokens(text string) int {
	var tokens float64
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r):
			tokens += 1.5
		case unicode.IsLetter(r):
			tokens += 0.25
		case unicode.IsDigit(r):
			tokens += 0.5
		case unicode.IsSpace(r):
			// 空格不计
		default:
			tokens += 0.5
		}
	}
	return int(math.Ceil(tokens))
}

// estimateHistoryTokens 估算"当前查询 + 历史"总 token 数。
// 用于改写/生成阶段的 Token 预算兜底（REQ-CHAT-006-A）。
func estimateHistoryTokens(history []*entity.Message, currentQuery string) int {
	total := estimateTokens(currentQuery)
	for _, m := range history {
		total += estimateTokens(m.Content)
	}
	return total
}

// trimHistoryForTokens FIFO 丢弃最早历史轮次直到 token 数 ≤ budget 或 history 长度 < 2。
// 每轮 = user + assistant 两条消息，丢弃 history[:2] 即丢一轮（GetRecentHistory 已按旧→新排序）。
// ponytail: 不裁剪中间——保留时间连续性，避免历史断档误导 LLM；
// 上限——若单条消息已超 budget，循环会清空 history 至 <2（仅保留当前查询，LLM 上下文为空）。
func trimHistoryForTokens(history []*entity.Message, currentQuery string, budget int) []*entity.Message {
	for len(history) >= 2 && estimateHistoryTokens(history, currentQuery) > budget {
		history = history[2:]
	}
	return history
}

// generationBudget 返回「历史 + 切片」可用的 token 预算：总预算扣除提示词与当前问题。
// 提示词体积不可控（管理员可配 4K 字人格 + 代码护栏），必须从预算里扣，不能只看历史与切片。
func generationBudget(systemPrompt, currentQuery string) int {
	budget := constants.TokenBudgetGenerate - estimateTokens(systemPrompt) - estimateTokens(currentQuery)
	if budget < minGenerationBudget {
		return minGenerationBudget
	}
	return budget
}

// trimChunksForTokens 按 token 预算从尾部（相似度最低）丢弃检索切片。
// 至少保留 1 条：切片是答案的唯一依据，全部丢掉会让模型无资料可依。
// 单条切片超预算时仍保留该条，由 platform/llm 的硬上限（maxContextRunes）作最后防线。
func trimChunksForTokens(chunks []rag.Chunk, budget int) []rag.Chunk {
	total := 0
	for _, c := range chunks {
		total += estimateTokens(c.Content)
	}
	for len(chunks) > 1 && total > budget {
		total -= estimateTokens(chunks[len(chunks)-1].Content)
		chunks = chunks[:len(chunks)-1]
	}
	return chunks
}

// truncateTitle 取前 N 个 rune 作为标题（REQ-CHAT-018）。
func truncateTitle(msg string) string {
	r := []rune(msg)
	if len(r) > entity.TitleMaxLen {
		r = r[:entity.TitleMaxLen]
	}
	return string(r)
}

// toLLMMessages 将 entity.Message 转换为 llm.Message。
func toLLMMessages(in []*entity.Message) []llm.Message {
	out := make([]llm.Message, 0, len(in))
	for _, m := range in {
		out = append(out, llm.Message{Role: m.Role, Content: m.Content})
	}
	return out
}

// chunkContents 提取（已按文章归并的）资料内容用于审核证据，过滤空/纯空白内容（编号由审核侧按序添加）。
func chunkContents(chunks []rag.Chunk) []string {
	out := make([]string, 0, len(chunks))
	for _, c := range chunks {
		if strings.TrimSpace(c.Content) == "" {
			continue
		}
		out = append(out, c.Content)
	}
	return out
}

// citedArticleCount 统计答案实际引用的文章数（按 article_id 去重；忽略越界/非法编号）。
// 用于观测"引用合规率"：候选 chunks 与答案引用对不上时便于排查（如模型漏标）。
func citedArticleCount(answer string, chunks []rag.Chunk) int {
	seen := make(map[string]struct{})
	for _, m := range citeMarkerRe.FindAllStringSubmatch(answer, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 1 || n > len(chunks) {
			continue
		}
		seen[chunks[n-1].ArticleID] = struct{}{}
	}
	return len(seen)
}

// groupChunksByArticle 按 article_id 归并命中切片，使引用编号以"文章"为单位。
// 目的：与前端"参考来源"列表（按 article_id 去重）语义一致——同一篇文章的多个命中切片
// 合并为一条，编号 = 文章首次出现顺序，避免"列表 1 条、正文却引用 [资料3]"的错位。
// 归并规则：内容按命中顺序拼接（纯空白内容跳过）；Score/VecScore 取最大；
// ContentRisk 任一为 high 则整体为 high（高风险判定不得被归并稀释）。
// 无 article_id 的切片无法确定同源，各自独立保留，保证内容不丢。
func groupChunksByArticle(chunks []rag.Chunk) []rag.Chunk {
	out := make([]rag.Chunk, 0, len(chunks))
	index := make(map[string]int, len(chunks))
	for _, c := range chunks {
		if strings.TrimSpace(c.Content) == "" {
			continue
		}
		key := strings.TrimSpace(c.ArticleID)
		if key == "" {
			out = append(out, c)
			continue
		}
		i, ok := index[key]
		if !ok {
			index[key] = len(out)
			out = append(out, c)
			continue
		}
		merged := &out[i]
		merged.Content = merged.Content + "\n" + c.Content
		if c.Score > merged.Score {
			merged.Score = c.Score
		}
		if c.VecScore > merged.VecScore {
			merged.VecScore = c.VecScore
		}
		if c.ContentRisk == contentRiskHigh {
			merged.ContentRisk = contentRiskHigh
		}
	}
	return out
}

// chunkContentsForLLM 构造送入生成模型的参考资料：每条（已按文章归并）资料加稳定编号 [资料N]
// 与文章标题，使模型可在答案中用 [资料N] 内联引用，且编号与审核侧证据、references 顺序、
// 前端参考来源列表保持一致（可溯源）。过滤规则与 chunkContents 完全一致，保证两侧"第 N 条"指向同一资料。
func chunkContentsForLLM(chunks []rag.Chunk) []string {
	out := make([]string, 0, len(chunks))
	idx := 0
	for _, c := range chunks {
		if strings.TrimSpace(c.Content) == "" {
			continue
		}
		idx++
		if title := strings.TrimSpace(c.ArticleTitle); title != "" {
			out = append(out, fmt.Sprintf("[资料%d] 《%s》\n%s", idx, title, c.Content))
			continue
		}
		out = append(out, fmt.Sprintf("[资料%d]\n%s", idx, c.Content))
	}
	return out
}

// toEntityRefs 将 rag.Chunk 转换为 entity.Reference（持久化到 messages.referenced_chunks）。
func toEntityRefs(chunks []rag.Chunk) []entity.Reference {
	out := make([]entity.Reference, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, entity.Reference{
			ChunkID:      c.ChunkID,
			ArticleID:    c.ArticleID,
			ArticleTitle: c.ArticleTitle,
			Content:      c.Content,
			Score:        c.Score,
		})
	}
	return out
}

// deptIDPtr 取检索使用的 deptID。用户显式选择"全部科室"（selectedDeptID=0）时返回 nil。
// 会话已锁定时优先用锁定值；未锁定时返回 nil（不限科室，检索全部可访问文章）。
func deptIDPtr(selectedDeptID *int64, conv *entity.Conversation) *int64 {
	// 用户显式选择"全部科室" → 不限科室
	if selectedDeptID != nil && *selectedDeptID == 0 {
		return nil
	}
	if conv.LockedDeptID != nil {
		return conv.LockedDeptID
	}
	return nil
}
