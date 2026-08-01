package adapter

import (
	"testing"

	"health-nexus/internal/platform/llm"
)

// TestBuildSwappableClients 构造 4 个 SwappableClient（初始均未配置）。
func TestBuildSwappableClients(t *testing.T) {
	sc := BuildSwappableClients()
	if sc.Chat == nil || sc.Embed == nil || sc.Rerank == nil || sc.Rewrite == nil {
		t.Error("BuildSwappableClients() returned nil field")
	}
	// 初始状态应该是未配置
	if sc.Chat.IsReady() {
		t.Error("BuildSwappableClients() Chat should not be ready initially")
	}
}

// TestSwappableClients_AsInterfaces 验证 SwappableClient 可以赋值给接口变量。
func TestSwappableClients_AsInterfaces(t *testing.T) {
	sc := llm.NewSwappableClient(nil)

	// 应该可以赋值给接口——编译期断言
	var _ llm.Streamer = sc
	var _ llm.Embedder = sc
	var _ llm.Rewriter = sc
	var _ llm.Reranker = sc
}

// TestSwappableClients_SwapEachCapability 验证 SwappableClients 可以独立 Swap 每个能力。
func TestSwappableClients_SwapEachCapability(t *testing.T) {
	sc := BuildSwappableClients()

	// Swap nil 不应 panic
	sc.Chat.Swap(nil)
	sc.Embed.Swap(nil)
	sc.Rerank.Swap(nil)
	sc.Rewrite.Swap(nil)

	if sc.Chat.IsReady() {
		t.Error("Chat should not be ready after Swap(nil)")
	}
}
