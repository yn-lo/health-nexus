package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// maxRerankBodyBytes 重排响应体读取上限（10 MiB），防止超大响应耗尽内存。
const maxRerankBodyBytes = 10 << 20

// RerankResult 重排结果：文档索引 + 相关性分数。
type RerankResult struct {
	Index int     // 文档在输入列表中的索引
	Score float64 // 相关性分数（0-1）
}

// Reranker 文档重排接口，用于混合检索后重排（REQ-WIKI-014）。
// 返回按相关性降序排列的结果，每个结果包含索引和分数。
type Reranker interface {
	Rerank(ctx context.Context, query string, documents []string, topK int) ([]RerankResult, error)
}

// rerankRequest SiliconFlow /v1/rerank 请求体。
type rerankRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	TopN      int      `json:"top_n,omitempty"`
}

// rerankAPIResponse SiliconFlow /v1/rerank 响应体。
type rerankAPIResponse struct {
	ID      string          `json:"id"`
	Results []rerankAPIItem `json:"results"`
}

// rerankAPIItem 单条重排结果。
type rerankAPIItem struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
}

// Rerank 调用 /v1/rerank 端点对文档按相关性重排。
// 支持 SiliconFlow、Jina 等提供原生 rerank API 的供应商。
// 返回按相关性降序排列的结果，每个结果包含索引和分数。
func (c *Client) Rerank(ctx context.Context, query string, documents []string, topK int) ([]RerankResult, error) {
	if c.chat == nil {
		return nil, ErrNotConfigured
	}
	if len(documents) == 0 {
		return nil, nil
	}
	if topK <= 0 || topK > len(documents) {
		topK = len(documents)
	}

	model := c.cfg.ChatModel // rerank 模型名映射到 ChatModel（见 NewRerankClient 注释）
	if model == "" {
		return nil, fmt.Errorf("rerank: model not configured")
	}

	reqBody := rerankRequest{
		Model:     model,
		Query:     query,
		Documents: documents,
		TopN:      topK,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("rerank: marshal request: %w", err)
	}

	// 构造请求 URL：BaseURL + /rerank（BaseURL 已含 /v1）
	reqURL := c.cfg.BaseURL + "/rerank"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("rerank: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rerank: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxRerankBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("rerank: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rerank: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp rerankAPIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("rerank: parse response: %w", err)
	}

	results := make([]RerankResult, 0, len(apiResp.Results))
	for _, item := range apiResp.Results {
		if item.Index < 0 || item.Index >= len(documents) {
			continue
		}
		results = append(results, RerankResult{
			Index: item.Index,
			Score: item.RelevanceScore,
		})
	}
	return results, nil
}
