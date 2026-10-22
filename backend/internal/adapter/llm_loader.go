// Package adapter 实现跨域适配器（DIP 原则：domain 定义接口，adapter 提供实现）。
package adapter

import (
	"fmt"
	"log/slog"
	"time"

	appconfig "health-nexus/internal/config"
	"health-nexus/internal/domain/config/entity"
	"health-nexus/internal/platform/crypto"
	"health-nexus/internal/platform/llm"
)

const llmClientTimeout = 60 * time.Second

// providerClient 配对实体与客户端，便于按实体字段做备用筛选。
type providerClient struct {
	p *entity.AIProvider
	c *llm.Client
}

// buildChainWithFallback 按 priority 顺序把同类型的启用 provider 组装成主备链。
// 无可用 DB provider（或全部构造失败）时回退到 config.yaml。
//
// sameVectorSpace 非 nil 时用于筛选备用 provider：embedding 只有同模型同维度才能参与
// 故障转移，否则向量空间不一致会让检索结果失真。
func buildChainWithFallback(
	providers []*entity.AIProvider,
	aesKey []byte,
	fallback appconfig.LLMConfig,
	newClient func(appconfig.LLMConfig) (*llm.Client, error),
	capability string,
	sameVectorSpace func(primary, candidate *entity.AIProvider) bool,
) (*llm.Client, error) {
	built := make([]providerClient, 0, len(providers))
	for _, p := range providers {
		c, err := buildClientFromEntity(p, aesKey)
		if err != nil {
			slog.Warn("llm: failed to build "+capability+" client from DB provider, skipped",
				"provider_id", p.ID, "priority", p.Priority, "err", err)
			continue
		}
		if !c.IsReady() {
			slog.Warn("llm: DB provider not ready (missing key/model), skipped",
				"provider_id", p.ID, "capability", capability)
			continue
		}
		built = append(built, providerClient{p: p, c: c})
	}
	if len(built) == 0 {
		c, err := newClient(fallback)
		if err != nil {
			return nil, fmt.Errorf("init fallback %s client: %w", capability, err)
		}
		return c, nil
	}

	primary := built[0]
	clients := make([]*llm.Client, 0, len(built))
	clients = append(clients, primary.c)
	for _, pc := range built[1:] {
		if sameVectorSpace != nil && !sameVectorSpace(primary.p, pc.p) {
			slog.Warn("llm: skip fallback provider with different vector space",
				"capability", capability, "provider_id", pc.p.ID,
				"primary_model", primary.p.ModelName, "candidate_model", pc.p.ModelName)
			continue
		}
		clients = append(clients, pc.c)
	}
	return llm.NewFailoverClient(clients...), nil
}

// buildClientFromEntity 解密 API Key + 构造 llm.Client。
// ponytail: 单一职责--只做"DB entity -> llm.Client"转换，不处理 fallback，简化。
func buildClientFromEntity(p *entity.AIProvider, aesKey []byte) (*llm.Client, error) {
	apiKey, err := crypto.Decrypt(string(p.APIKeyEncrypted), aesKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt api key: %w", err)
	}
	return llm.NewClientFromProvider(
		p.ProviderType, p.APIURL, apiKey, p.ModelName,
		llmClientTimeout, p.Parameters, p.IsFullURL,
	), nil
}

// sameEmbeddingVectorSpace 判断两个 embedding provider 是否产出可比向量：同模型且同维度。
func sameEmbeddingVectorSpace(primary, candidate *entity.AIProvider) bool {
	return primary.ModelName == candidate.ModelName && dimensionOf(primary) == dimensionOf(candidate)
}

func dimensionOf(p *entity.AIProvider) int {
	if p.Dimension == nil {
		return 0
	}
	return *p.Dimension
}
