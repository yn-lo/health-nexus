// Package adapter 提供 asynq 任务入队适配器。
package adapter

import (
	"context"
	"fmt"
	"strconv"

	configservice "health-nexus/internal/domain/config/service"
	wikiservice "health-nexus/internal/domain/wiki/service"
	"health-nexus/internal/platform/asynq"

	asynqlib "github.com/hibiken/asynq"
)

// AsynqVectorizeEnqueuer 实现 wiki/service.VectorizeEnqueuer。
// 桥接 wiki 域（int64 articleID）到 asynq 任务队列。
type AsynqVectorizeEnqueuer struct {
	client *asynqlib.Client
}

// NewAsynqVectorizeEnqueuer 构造适配器。
func NewAsynqVectorizeEnqueuer(client *asynqlib.Client) *AsynqVectorizeEnqueuer {
	return &AsynqVectorizeEnqueuer{client: client}
}

// Enqueue 将文章 ID 序列化为 payload 并入队向量化任务。
// ponytail: articles.id 是 BIGSERIAL（int64），payload 直接用 int64 的字符串形式，简化；
// worker 端解析时也按 int64 处理；asynq 包仅暴露 TaskVectorizeArticle 常量，入队逻辑由本 adapter 实现。
func (e *AsynqVectorizeEnqueuer) Enqueue(ctx context.Context, articleID int64) error {
	payload := strconv.FormatInt(articleID, 10)
	task := asynqlib.NewTask(asynq.TaskVectorizeArticle, []byte(payload))
	_, err := e.client.EnqueueContext(ctx, task)
	if err != nil {
		return fmt.Errorf("enqueue vectorize task (articleID=%d): %w", articleID, err)
	}
	return nil
}

// 编译期断言。
var _ wikiservice.VectorizeEnqueuer = (*AsynqVectorizeEnqueuer)(nil)

// AsynqReviewNotifyEnqueuer 实现 wiki/service.ReviewNotifyEnqueuer。
// 桥接 wiki 域（int64 articleID）到 asynq TaskReviewNotify 任务队列。
type AsynqReviewNotifyEnqueuer struct {
	client *asynqlib.Client
}

// NewAsynqReviewNotifyEnqueuer 构造复审通知入队适配器。
func NewAsynqReviewNotifyEnqueuer(client *asynqlib.Client) *AsynqReviewNotifyEnqueuer {
	return &AsynqReviewNotifyEnqueuer{client: client}
}

// Enqueue 将文章 ID 序列化为 payload 并入队复审通知任务。
// 通知系统未实现，worker 端 handler 仅记录 slog 占位（Critical 1 修复说明）。
func (e *AsynqReviewNotifyEnqueuer) Enqueue(ctx context.Context, articleID int64) error {
	payload := strconv.FormatInt(articleID, 10)
	task := asynqlib.NewTask(asynq.TaskReviewNotify, []byte(payload))
	_, err := e.client.EnqueueContext(ctx, task)
	if err != nil {
		return fmt.Errorf("enqueue review notify task (articleID=%d): %w", articleID, err)
	}
	return nil
}

// 编译期断言。
var _ wikiservice.ReviewNotifyEnqueuer = (*AsynqReviewNotifyEnqueuer)(nil)

// ConfigRAGConfigProvider 实现 wikiservice.RAGConfigProvider。
// 桥接 config 域 ConfigService.GetRAGConfig 返回的 RAGConfigResponse 到 wiki 域本地 DTO，
// 避免 wiki/service 直接 import config/entity（AC-ARCH-02）。
type ConfigRAGConfigProvider struct {
	svc *configservice.ConfigService
}

// NewConfigRAGConfigProvider 构造 RAG 配置适配器。
func NewConfigRAGConfigProvider(svc *configservice.ConfigService) *ConfigRAGConfigProvider {
	return &ConfigRAGConfigProvider{svc: svc}
}

// GetRAGConfig 桥接 config 域 RAGConfigResponse 到 wiki 域 RAGSearchConfig。
func (p *ConfigRAGConfigProvider) GetRAGConfig(ctx context.Context) (*wikiservice.RAGSearchConfig, error) {
	resp, err := p.svc.GetRAGConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("get rag config: %w", err)
	}
	if resp == nil {
		return nil, nil
	}
	return &wikiservice.RAGSearchConfig{
		TopK:                resp.TopK,
		SimilarityThreshold: resp.SimilarityThreshold,
		RerankEnabled:       resp.RerankEnabled,
		RerankThreshold:     resp.RerankThreshold,
		MaxChunks:           resp.MaxChunks,
		DiversityFactor:     resp.DiversityFactor,
		ChunkSize:           resp.ChunkSize,
		ChunkOverlap:        resp.ChunkOverlap,
		OODThreshold:        resp.OODThreshold,
	}, nil
}

// 编译期断言。
var _ wikiservice.RAGConfigProvider = (*ConfigRAGConfigProvider)(nil)
