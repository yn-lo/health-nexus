// Package llm 封装 LLM 客户端，提供对话流式生成、向量生成、查询改写和文档重排能力。
// 底层基于 OpenAI 兼容 API，支持 DeepSeek、通义千问等 Provider。
package llm

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"

	"health-nexus/internal/config"
	"health-nexus/internal/shared/constants"
)

// ErrNotConfigured LLM 客户端未配置 API Key 时返回的错误。
// 调用方（chat/wiki）应据此降级：chat 返回 503，wiki 检索返回空切片。
var ErrNotConfigured = errors.New("llm: API key not configured, set via admin UI or HEALTH_NEXUS_LLM_API_KEY")

// Client LLM 客户端，封装 OpenAI 兼容连接与模型配置。
// chat==nil 表示 API Key 未配置（未就绪状态），所有方法调用返回 ErrNotConfigured。
// params 供应商扩展参数（temperature / top_p / max_tokens / response_format），按请求注入。
type Client struct {
	chat       *openai.Client
	cfg        config.LLMConfig
	params     map[string]any
	httpClient *http.Client // 用于 Rerank() 直接调 /v1/rerank（go-openai 不暴露 HTTPClient）
}

// HTTP transport 超时加固参数（newOpenAIClient 用）。
const (
	httpIdleConnTimeout       = 90 * time.Second
	httpTLSHandshakeTimeout   = 10 * time.Second
	httpExpectContinueTimeout = 1 * time.Second
)

// normalizeBaseURL 规范化 API Base URL：确保以 /v1 结尾，去掉多余的 API 路径后缀。
// go-openai 库期望 BaseURL 到 /v1 这一层，内部会拼接 /chat/completions 等路径。
// 用户可能输入：
//   - https://api.siliconflow.cn          → 缺少 /v1，需追加
//   - https://api.siliconflow.cn/v1       → 正确
//   - https://api.siliconflow.cn/v1/chat/completions → 多余后缀，需截断到 /v1
func normalizeBaseURL(baseURL string) string {
	if baseURL == "" {
		return ""
	}
	trimmed := strings.TrimRight(baseURL, "/")
	// 常见多余后缀：用户可能粘贴了完整 API 路径，需要截断到 /v1
	suffixes := []string{
		"/chat/completions",
		"/completions",
		"/embeddings",
		"/rerank",
	}
	for _, s := range suffixes {
		if idx := strings.Index(trimmed, s); idx > 0 {
			trimmed = trimmed[:idx]
		}
	}
	// 去掉截断后可能残留的尾斜杠
	trimmed = strings.TrimRight(trimmed, "/")
	if strings.HasSuffix(trimmed, "/v1") {
		return trimmed
	}
	return trimmed + "/v1"
}

// newOpenAIClient 工厂：依据 baseURL/apiKey/timeout 构造 OpenAI 兼容客户端。
// Transport 加固无条件设置——R8-Config-5 修复：TLS 握手/连接复用/HTTP2 必须始终启用；
// ResponseHeaderTimeout 单独条件化：仅在 timeout>0 时启用（避免 0 值导致首字节等待无限期）。
func newOpenAIClient(
	baseURL, apiKey string, timeout time.Duration,
) (client *openai.Client, hc *http.Client) {
	ocfg := openai.DefaultConfig(apiKey)
	if normalized := normalizeBaseURL(baseURL); normalized != "" {
		ocfg.BaseURL = normalized
	}
	transport := &http.Transport{
		IdleConnTimeout:       httpIdleConnTimeout,
		TLSHandshakeTimeout:   httpTLSHandshakeTimeout,
		ExpectContinueTimeout: httpExpectContinueTimeout,
		ForceAttemptHTTP2:     true,
	}
	if timeout > 0 {
		transport.ResponseHeaderTimeout = timeout
	}
	hc = &http.Client{Transport: transport}
	ocfg.HTTPClient = hc
	client = openai.NewClientWithConfig(ocfg)
	return client, hc
}

// NewClient 依据配置创建 LLM 主客户端（chat provider），支持 OpenAI 兼容 API。
// 当 cfg.APIKey 非空时创建就绪客户端；为空时创建未就绪客户端（chat==nil），
// 服务器可正常启动，管理员通过后端 UI 配置 AI Provider 后再使用。
// REQ-NFR-015：LLM 不可用时必须降级，不得阻断启动。
func NewClient(cfg config.LLMConfig) (*Client, error) {
	if cfg.APIKey == "" {
		slog.Warn("llm: API key not configured, " +
			"chat/embed/rerank will be unavailable until admin configures AI provider")
		return &Client{chat: nil, cfg: cfg}, nil
	}
	cfg.BaseURL = normalizeBaseURL(cfg.BaseURL)
	chat, hc := newOpenAIClient(cfg.BaseURL, cfg.APIKey, cfg.Timeout)
	return &Client{chat: chat, cfg: cfg, httpClient: hc}, nil
}

// NewEmbeddingClient 依据 cfg.Embedding 子配置创建独立 embedding 客户端（OpenAI 兼容）。
// 子配置零值字段回退到主配置（BaseURL/APIKey/Timeout 通用，Model→EmbeddingModel）。
// API key 为空（含回退后）时返回 nil，让上层（SearchService/VectorizeHandler）走降级路径。
func NewEmbeddingClient(cfg config.LLMConfig) (*Client, error) {
	baseURL, apiKey, model, timeout := resolveProvider(
		cfg.Embedding, cfg.BaseURL, cfg.APIKey, cfg.EmbeddingModel, cfg.Timeout,
	)
	if apiKey == "" {
		slog.Warn("llm: embedding API key not configured, embedding will be unavailable")
		return nil, nil
	}
	derived := cfg
	derived.BaseURL = normalizeBaseURL(baseURL)
	derived.APIKey = apiKey
	derived.EmbeddingModel = model
	derived.Timeout = timeout
	chat, hc := newOpenAIClient(baseURL, apiKey, timeout)
	return &Client{chat: chat, cfg: derived, httpClient: hc}, nil
}

// NewRewriteClient 依据 cfg.Rewrite 子配置创建独立 rewrite 客户端（OpenAI 兼容）。
// 子配置零值字段回退到主配置（BaseURL/APIKey/Timeout 通用，Model→RewriteModel）。
// API key 为空（含回退后）时返回 nil，让上层（di 装配）回退到主 chat client。
func NewRewriteClient(cfg config.LLMConfig) (*Client, error) {
	baseURL, apiKey, model, timeout := resolveProvider(
		cfg.Rewrite, cfg.BaseURL, cfg.APIKey, cfg.RewriteModel, cfg.Timeout)
	if apiKey == "" {
		slog.Warn("llm: rewrite API key not configured, rewrite will fallback to main chat client")
		return nil, nil
	}
	derived := cfg
	derived.BaseURL = normalizeBaseURL(baseURL)
	derived.APIKey = apiKey
	derived.RewriteModel = model
	derived.Timeout = timeout
	chat, hc := newOpenAIClient(baseURL, apiKey, timeout)
	return &Client{chat: chat, cfg: derived, httpClient: hc}, nil
}

// NewRerankClient 依据 cfg.Rerank 子配置创建独立 rerank 客户端（原生 /v1/rerank API）。
// 子配置零值字段回退到主配置（BaseURL/APIKey/Timeout 通用，Model 无主字段回退，必须显式配置）。
// API key 为空（含回退后）时返回 nil，让上层（SearchService）走降级路径（RRF 顺序）。
//
// 注：rerank 模型名映射到 cfg.ChatModel——Client.Rerank 内部用 c.cfg.ChatModel 调用 /v1/rerank，
// 语义上"这个 client 的主模型就是 rerank 模型"，避免给 Client 加额外的 rerankModel 字段。
func NewRerankClient(cfg config.LLMConfig) (*Client, error) {
	baseURL, apiKey, model, timeout := resolveProvider(cfg.Rerank, cfg.BaseURL, cfg.APIKey, "", cfg.Timeout)
	if apiKey == "" || model == "" {
		slog.Warn("llm: rerank API key or model not configured, rerank will be unavailable")
		return nil, nil
	}
	derived := cfg
	derived.BaseURL = normalizeBaseURL(baseURL)
	derived.APIKey = apiKey
	derived.ChatModel = model // rerank model 映射到 ChatModel，供 Client.Rerank 使用
	derived.Timeout = timeout
	chat, hc := newOpenAIClient(baseURL, apiKey, timeout)
	return &Client{chat: chat, cfg: derived, httpClient: hc}, nil
}

// NewClientFromProvider 依据 DB 中的 AIProvider 实体构造客户端（方案 C：DB 配置真正生效）。
// 用途：di 启动时从 DB 读 active provider 构造 4 个客户端（chat/embed/rerank/rewrite）。
// providerType 决定 model 映射到 ChatModel/EmbeddingModel/RewriteModel 的哪个字段。
// params 供应商扩展参数（temperature / top_p / max_tokens / response_format），nil 表示不注入。
//
// ponytail: 不依赖 config.LLMConfig，纯按 provider 实体构造，简化；
// 单 client 只承载一个能力，model 字段映射由 providerType 内部决定。
func NewClientFromProvider(
	providerType, baseURL, apiKey, model string, timeout time.Duration, params map[string]any,
) *Client {
	if apiKey == "" || model == "" {
		return &Client{chat: nil, cfg: config.LLMConfig{}}
	}
	normalized := normalizeBaseURL(baseURL)
	derived := config.LLMConfig{
		BaseURL: normalized,
		APIKey:  apiKey,
		Timeout: timeout,
	}
	switch providerType {
	case constants.ProviderTypeLLM, constants.ProviderTypeRerank:
		derived.ChatModel = model // chat 主模型；rerank 复用 ChatModel（见 NewRerankClient 注释）
	case constants.ProviderTypeEmbedding:
		derived.EmbeddingModel = model
	case constants.ProviderTypeRewrite:
		derived.RewriteModel = model
	}
	chat, hc := newOpenAIClient(baseURL, apiKey, timeout)
	return &Client{chat: chat, cfg: derived, params: params, httpClient: hc}
}

// resolveProvider 解析 ProviderConfig：零值字段回退到主配置对应字段。
// fallbackModel 由调用方按能力传入（Embedding→EmbeddingModel，Rewrite→RewriteModel，Rerank→""）。
func resolveProvider(
	pc config.ProviderConfig, fallbackBaseURL, fallbackAPIKey, fallbackModel string, fallbackTimeout time.Duration,
) (baseURL, apiKey, model string, timeout time.Duration) {
	baseURL = pc.BaseURL
	if baseURL == "" {
		baseURL = fallbackBaseURL
	}
	apiKey = pc.APIKey
	if apiKey == "" {
		apiKey = fallbackAPIKey
	}
	model = pc.Model
	if model == "" {
		model = fallbackModel
	}
	timeout = pc.Timeout
	if timeout == 0 {
		timeout = fallbackTimeout
	}
	return
}

// ChatModel 返回主对话模型名。
func (c *Client) ChatModel() string { return c.cfg.ChatModel }

// IsReady 返回客户端是否已配置 API Key 可用。
// 未就绪（chat==nil）时所有 LLM 调用方法返回 ErrNotConfigured。
// 调用方（如 di 层装配 LLMSafetyChecker）可据此决定是否注入 LLM 依赖。
func (c *Client) IsReady() bool { return c.chat != nil }

// RewriteModel 返回改写用小模型名。
func (c *Client) RewriteModel() string { return c.cfg.RewriteModel }

// EmbeddingModel 返回 embedding 模型名。
func (c *Client) EmbeddingModel() string { return c.cfg.EmbeddingModel }

// 编译期断言：Client 实现全部对外接口，签名漂移时立即编译失败。
var (
	_ Streamer = (*Client)(nil)
	_ Embedder = (*Client)(nil)
	_ Rewriter = (*Client)(nil)
	_ Reranker = (*Client)(nil)
)
