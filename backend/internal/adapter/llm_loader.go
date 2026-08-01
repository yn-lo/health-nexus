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

func buildClientWithFallback(
	byType map[string]*entity.AIProvider,
	providerType string,
	aesKey []byte,
	fallback appconfig.LLMConfig,
	newClient func(appconfig.LLMConfig) (*llm.Client, error),
	capability string,
) (*llm.Client, error) {
	if p, ok := byType[providerType]; ok {
		if c, err := buildClientFromEntity(p, aesKey); err != nil {
			slog.Warn("llm: failed to build "+capability+" client from DB provider, fallback to config.yaml",
				"provider_id", p.ID, "err", err)
		} else {
			return c, nil
		}
	}
	c, err := newClient(fallback)
	if err != nil {
		return nil, fmt.Errorf("init fallback %s client: %w", capability, err)
	}
	return c, nil
}

// buildClientFromEntity 解密 API Key + 构造 llm.Client。
// ponytail: 单一职责--只做"DB entity -> llm.Client"转换，不处理 fallback，简化。
func buildClientFromEntity(p *entity.AIProvider, aesKey []byte) (*llm.Client, error) {
	apiKey, err := crypto.Decrypt(string(p.APIKeyEncrypted), aesKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt api key: %w", err)
	}
	return llm.NewClientFromProvider(p.ProviderType, p.APIURL, apiKey, p.ModelName, llmClientTimeout, p.Parameters), nil
}
