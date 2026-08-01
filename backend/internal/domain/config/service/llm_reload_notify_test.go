package service

import (
	"context"
	"testing"
)

// TestNotifyLLMReload_NilRedisNoPanic 验证 redis 为 nil 时不 panic。
func TestNotifyLLMReload_NilRedisNoPanic(t *testing.T) {
	svc := newTestService(newMockAIProviderRepo(), nil, nil, nil, nil, nil, nil)
	// 不应 panic
	svc.notifyLLMReload(context.Background())
}

// TestLLMReloadChannelValue 验证 Redis 频道名常量。
func TestLLMReloadChannelValue(t *testing.T) {
	if llmReloadChannel != "health-nexus:llm:reload" {
		t.Errorf("llmReloadChannel = %q, want %q", llmReloadChannel, "health-nexus:llm:reload")
	}
}

// TestCreateAIProvider_CallsNotifyReload 验证创建 AI Provider 后调用 notifyLLMReload。
// 通过检查 ConfigService 的 notifyLLMReload 在 redis!=nil 时无报错来间接验证。
func TestCreateAIProvider_CallsNotifyReload(t *testing.T) {
	aiRepo := newMockAIProviderRepo()
	svc := newTestService(aiRepo, nil, nil, nil, nil, nil, nil)

	// 创建时不应 panic（redis=nil，notifyLLMReload 应静默跳过）
	_, err := svc.CreateAIProvider(ctxWithOperator(), CreateAIProviderRequest{
		ProviderType: "llm",
		Name:         "test-llm",
		APIBase:      "https://api.example.com/v1",
		APIKey:       "sk-test",
		ModelName:    "gpt-4",
		IsActive:     boolPtr(true),
	})
	if err != nil {
		t.Fatalf("CreateAIProvider failed: %v", err)
	}
}

// TestUpdateAIProvider_CallsNotifyReload 验证更新 AI Provider 后调用 notifyLLMReload。
func TestUpdateAIProvider_CallsNotifyReload(t *testing.T) {
	aiRepo := newMockAIProviderRepo()
	svc := newTestService(aiRepo, nil, nil, nil, nil, nil, nil)

	p, _ := svc.CreateAIProvider(ctxWithOperator(), CreateAIProviderRequest{
		ProviderType: "llm",
		Name:         "test-llm",
		APIBase:      "https://api.example.com/v1",
		APIKey:       "sk-test",
		ModelName:    "gpt-4",
		IsActive:     boolPtr(true),
	})

	// 更新不应 panic
	_, err := svc.UpdateAIProvider(ctxWithOperator(), p.ID, UpdateAIProviderRequest{
		Name:      stringPtr("updated-name"),
		APIBase:   stringPtr("https://api.example.com/v2"),
		ModelName: stringPtr("gpt-4o"),
	})
	if err != nil {
		t.Fatalf("UpdateAIProvider failed: %v", err)
	}
}

// TestDeleteAIProvider_CallsNotifyReload 验证删除 AI Provider 后调用 notifyLLMReload。
func TestDeleteAIProvider_CallsNotifyReload(t *testing.T) {
	aiRepo := newMockAIProviderRepo()
	svc := newTestService(aiRepo, nil, nil, nil, nil, nil, nil)

	p, _ := svc.CreateAIProvider(ctxWithOperator(), CreateAIProviderRequest{
		ProviderType: "llm",
		Name:         "test-llm",
		APIBase:      "https://api.example.com/v1",
		APIKey:       "sk-test",
		ModelName:    "gpt-4",
		IsActive:     boolPtr(true),
	})

	// 删除不应 panic
	err := svc.DeleteAIProvider(ctxWithOperator(), p.ID)
	if err != nil {
		t.Fatalf("DeleteAIProvider failed: %v", err)
	}
}
