package llm

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"health-nexus/internal/config"
)

// --- SwappableClient 基础行为测试 ---

func TestSwappableClient_DelegatesToInitialClient(t *testing.T) {
	initial := &Client{chat: nil, cfg: config.LLMConfig{ChatModel: "test-model"}}
	sc := NewSwappableClient(initial)

	if sc.IsReady() {
		t.Error("nil chat should not be ready")
	}
	if model := sc.ChatModel(); model != "test-model" {
		t.Errorf("ChatModel() = %q, want %q", model, "test-model")
	}
}

func TestSwappableClient_SwapUpdatesClient(t *testing.T) {
	old := &Client{chat: nil, cfg: config.LLMConfig{ChatModel: "old"}}
	sc := NewSwappableClient(old)

	newClient := &Client{chat: nil, cfg: config.LLMConfig{ChatModel: "new"}}
	sc.Swap(newClient)

	if model := sc.ChatModel(); model != "new" {
		t.Errorf("ChatModel() after Swap = %q, want %q", model, "new")
	}
}

func TestSwappableClient_SwapNilUsesNotReady(t *testing.T) {
	initial := &Client{chat: nil, cfg: config.LLMConfig{ChatModel: "old"}}
	sc := NewSwappableClient(initial)

	sc.Swap(nil)
	if sc.IsReady() {
		t.Error("IsReady() after Swap(nil) = true, want false")
	}
}

func TestSwappableClient_SwapNilReturnsErrNotConfigured(t *testing.T) {
	sc := NewSwappableClient(nil)

	_, err := sc.Embed(context.Background(), []string{"test"})
	if err != ErrNotConfigured {
		t.Errorf("Embed() after Swap(nil) err = %v, want ErrNotConfigured", err)
	}
}

func TestSwappableClient_SwapNilStreamErrNotConfigured(t *testing.T) {
	sc := NewSwappableClient(nil)

	_, err := sc.StreamChat(context.Background(), ChatRequest{})
	if err != ErrNotConfigured {
		t.Errorf("StreamChat() after Swap(nil) err = %v, want ErrNotConfigured", err)
	}
}

func TestSwappableClient_SwapNilRerankErrNotConfigured(t *testing.T) {
	sc := NewSwappableClient(nil)

	_, err := sc.Rerank(context.Background(), "q", []string{"doc"}, 1)
	if err != ErrNotConfigured {
		t.Errorf("Rerank() err = %v, want ErrNotConfigured", err)
	}
}

func TestSwappableClient_ConcurrentSwapAndRead(t *testing.T) {
	sc := NewSwappableClient(&Client{chat: nil, cfg: config.LLMConfig{ChatModel: "v0"}})

	var wg sync.WaitGroup
	const readers = 50
	const swappers = 10

	wg.Add(readers + swappers)

	for i := 0; i < readers; i++ {
		go func() {
			defer wg.Done()
			_ = sc.IsReady()
			_ = sc.ChatModel()
			_ = sc.EmbeddingModel()
		}()
	}
	for i := 0; i < swappers; i++ {
		go func(i int) {
			defer wg.Done()
			sc.Swap(&Client{
				chat: nil,
				cfg:  config.LLMConfig{ChatModel: "v" + string(rune('0'+i))},
			})
		}(i)
	}

	wg.Wait()
}

// --- SwappableClients 容器测试 ---

func TestSwappableClients_SwapEachCapability(t *testing.T) {
	sc := &SwappableClients{
		Chat:   NewSwappableClient(&Client{chat: nil, cfg: config.LLMConfig{ChatModel: "chat-old"}}),
		Embed:  NewSwappableClient(&Client{chat: nil, cfg: config.LLMConfig{EmbeddingModel: "embed-old"}}),
		Rerank: NewSwappableClient(&Client{chat: nil, cfg: config.LLMConfig{ChatModel: "rerank-old"}}),
	}

	sc.Chat.Swap(&Client{chat: nil, cfg: config.LLMConfig{ChatModel: "chat-new"}})
	sc.Embed.Swap(&Client{chat: nil, cfg: config.LLMConfig{EmbeddingModel: "embed-new"}})
	sc.Rerank.Swap(&Client{chat: nil, cfg: config.LLMConfig{ChatModel: "rerank-new"}})

	if sc.Chat.ChatModel() != "chat-new" {
		t.Errorf("Chat.ChatModel() = %q, want %q", sc.Chat.ChatModel(), "chat-new")
	}
	if sc.Embed.EmbeddingModel() != "embed-new" {
		t.Errorf("Embed.EmbeddingModel() = %q, want %q", sc.Embed.EmbeddingModel(), "embed-new")
	}
	if sc.Rerank.ChatModel() != "rerank-new" {
		t.Errorf("Rerank.ChatModel() = %q, want %q", sc.Rerank.ChatModel(), "rerank-new")
	}
}

// --- 编译期断言 ---

// TestSwappableClient_EmbedWithModel_SingleSnapshot P2：EmbedWithModel 在单次调用内只 Load 一次，
// 保证"生成向量 + 返回模型名"来自同一客户端快照（热切换下不得混用模型）。
// 未配置 client 时 fail-closed（返回 ErrNotConfigured + 空模型），绝不返回与实际向量不符的模型名。
func TestSwappableClient_EmbedWithModel_SingleSnapshot(t *testing.T) {
	sc := NewSwappableClient(&Client{chat: nil, cfg: config.LLMConfig{EmbeddingModel: "embed-A"}})

	_, model, err := sc.EmbedWithModel(context.Background(), []string{"x"})
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("未配置时 EmbedWithModel err = %v, want ErrNotConfigured", err)
	}
	// 生成失败时不得返回任何模型标识（避免下游按"未生成向量的模型"记录/检索）。
	if model != "" {
		t.Errorf("未配置时 model 应为空串，实际 %q", model)
	}

	// Swap 到未配置客户端同样 fail-closed（不存在"读到旧模型名"的窗口）。
	sc.Swap(nil)
	_, model, err = sc.EmbedWithModel(context.Background(), []string{"x"})
	if !errors.Is(err, ErrNotConfigured) || model != "" {
		t.Errorf("Swap(nil) 后应为 (ErrNotConfigured, \"\")，实际 err=%v model=%q", err, model)
	}
}

func TestSwappableClient_ImplementsInterfaces(t *testing.T) {
	var _ Streamer = (*SwappableClient)(nil)
	var _ Embedder = (*SwappableClient)(nil)
	var _ EmbedderWithModel = (*SwappableClient)(nil)
	var _ Reranker = (*SwappableClient)(nil)
	var _ JSONCompleter = (*SwappableClient)(nil)
}

// ensure atomic import is used
var _ atomic.Pointer[Client]
