// Package adapter 提供 config 域到 chat 域的安全规则适配器。
package adapter

import (
	"context"
	"fmt"
	"sync"
	"time"

	"health-nexus/internal/domain/config/entity"
	configservice "health-nexus/internal/domain/config/service"
	"health-nexus/internal/shared/constants"
	"health-nexus/internal/shared/pagination"
	"health-nexus/internal/shared/rag"
)

// sensitiveWordsCacheTTL 敏感词进程内缓存 TTL（D-MED-09 修复）。
// 与 RAGConfig/SafetyMessages 的 Redis cache-aside TTL 对齐（5min）。
const sensitiveWordsCacheTTL = 5 * time.Minute

const fullScanPageSize = 100

// ConfigSafetyRuleProvider 实现 rag.SafetyRuleProvider。
// 从 config 域加载敏感词和安全话术，供 chat 域输入安全审查使用。
//
// D-MED-09: SensitiveWords 走进程内 cache-aside——首次加载后 5min 内直接返回缓存，
// 避免每次 chat 请求都全量分页轮询 DB（每类 100/页 → 3 类 × N 页 SQL）。
// OutputSafetyRules 同理缓存，避免每次输出审查回源 DB + 重编译正则。
type ConfigSafetyRuleProvider struct {
	svc *configservice.ConfigService

	mu          sync.Mutex
	cachedWords *rag.SensitiveWords
	cachedAt    time.Time

	cachedRules   []rag.OutputSafetyRule
	rulesCachedAt time.Time
}

// NewConfigSafetyRuleProvider 构造适配器。
func NewConfigSafetyRuleProvider(svc *configservice.ConfigService) *ConfigSafetyRuleProvider {
	return &ConfigSafetyRuleProvider{svc: svc}
}

// SensitiveWords 加载三类敏感词（全量分页加载，不截断）。
// D-MED-09: 进程内 cache-aside——TTL 内直接返回缓存；过期或首次访问回源 DB 全量加载。
//
// ponytail: 进程内缓存无主动失效——上限：管理员更新敏感词后最多 5min 内生效
// （TTL 过期后下一次 chat 请求触发回源）。升级路径：ConfigService 写敏感词时通过
// 通知机制（如 Redis pub/sub 或本地 listener）主动失效本缓存；当前对齐 RAGConfig
// 的 5min TTL 模型，简单且与现有 cache-aside 一致。
// 并发回源：mu 仅保护缓存读写，回源 DB 在锁内执行——简化实现，避免多 goroutine 同时
// 打 DB；上限：首次或过期后的第一次请求会阻塞同进程其他 SensitiveWords 调用直到 DB 返回。
// 升级路径：singleflight 解耦锁与回源。
func (p *ConfigSafetyRuleProvider) SensitiveWords(ctx context.Context) (rag.SensitiveWords, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cachedWords != nil && time.Since(p.cachedAt) < sensitiveWordsCacheTTL {
		return *p.cachedWords, nil
	}
	words, err := p.loadSensitiveWords(ctx)
	if err != nil {
		// 回源失败：若有过期缓存可用，降级返回旧值（宁可延迟生效也不让 chat 不可用）。
		if p.cachedWords != nil {
			return *p.cachedWords, nil
		}
		return words, err
	}
	p.cachedWords = &words
	p.cachedAt = time.Now()
	return words, nil
}

// loadSensitiveWords 全量分页加载三类敏感词。
func (p *ConfigSafetyRuleProvider) loadSensitiveWords(ctx context.Context) (rag.SensitiveWords, error) {
	policy, err := p.svc.GetSafetyPolicy(ctx)
	if err != nil {
		return rag.SensitiveWords{}, fmt.Errorf("load safety policy: %w", err)
	}
	return rag.SensitiveWords{
		Suicide:   policy.InputSensitiveWords.Suicide.Words,
		Emergency: policy.InputSensitiveWords.Emergency.Words,
		Injection: policy.InputSensitiveWords.Injection.Words,
	}, nil
}

// RejectionMessage 拒答话术。
func (p *ConfigSafetyRuleProvider) RejectionMessage() string {
	return p.loadMessage(
		context.Background(), entity.SafetyMessageTypeRejection,
		configservice.DefaultSafetyMessages.RejectionMessage,
	)
}

// NoKnowledgeMessage 无知识话术。
func (p *ConfigSafetyRuleProvider) NoKnowledgeMessage() string {
	return p.loadMessage(
		context.Background(), entity.SafetyMessageTypeNoKnowledge,
		configservice.DefaultSafetyMessages.NoKnowledgeMessage,
	)
}

// SystemErrorMessage 系统异常话术。
func (p *ConfigSafetyRuleProvider) SystemErrorMessage() string {
	return p.loadMessage(
		context.Background(), entity.SafetyMessageTypeSystemError,
		configservice.DefaultSafetyMessages.SystemErrorMessage,
	)
}

// EmergencyMessage 紧急就医话术。
func (p *ConfigSafetyRuleProvider) EmergencyMessage() string {
	return p.loadMessage(
		context.Background(), entity.SafetyMessageTypeEmergency,
		configservice.DefaultSafetyMessages.EmergencyMessage,
	)
}

// SafetyWarningMessage 安全警告话术。
func (p *ConfigSafetyRuleProvider) SafetyWarningMessage() string {
	return p.loadMessage(
		context.Background(), entity.SafetyMessageTypeSafetyWarning,
		configservice.DefaultSafetyMessages.SafetyWarningMessage,
	)
}

// CrisisResponse 危机响应话术。
// 从 safety_messages 表读取 crisis_response 类型；DB 故障时降级为硬编码默认（D-HIGH-04 修复）。
func (p *ConfigSafetyRuleProvider) CrisisResponse() string {
	return p.loadMessage(
		context.Background(), entity.SafetyMessageTypeCrisisResponse,
		configservice.DefaultSafetyMessages.CrisisResponse,
	)
}

// OutputSafetyRules 返回已启用的输出审查规则。
// 进程内 cache-aside（TTL 与 SensitiveWords 对齐）：避免每次输出审查回源 DB + 重编译正则。
// 回源失败时降级返回过期缓存（宁可延迟生效也不让 chat 不可用）。
func (p *ConfigSafetyRuleProvider) OutputSafetyRules(ctx context.Context) ([]rag.OutputSafetyRule, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cachedRules != nil && time.Since(p.rulesCachedAt) < sensitiveWordsCacheTTL {
		return p.cachedRules, nil
	}
	policy, err := p.svc.GetSafetyPolicy(ctx)
	if err != nil {
		if p.cachedRules != nil {
			return p.cachedRules, nil
		}
		return nil, err
	}
	rules := make([]rag.OutputSafetyRule, 0, len(policy.OutputRules))
	for _, rule := range policy.OutputRules {
		rules = append(rules, rag.OutputSafetyRule{
			Category: rule.Category, Pattern: rule.Pattern, Action: rule.Action, Replacement: rule.Replacement,
		})
	}
	p.cachedRules = rules
	p.rulesCachedAt = time.Now()
	return rules, nil
}

// OutputSafetyMessages 返回输出审查所需的实际话术。
func (p *ConfigSafetyRuleProvider) OutputSafetyMessages(ctx context.Context) (rag.OutputSafetyMessages, error) {
	messages, err := p.svc.GetSafetyMessages(ctx)
	if err != nil {
		return rag.OutputSafetyMessages{}, err
	}
	return rag.OutputSafetyMessages{
		RejectionMessage:     messages.RejectionMessage,
		SafetyWarningMessage: messages.SafetyWarningMessage,
	}, nil
}

// loadMessage 加载安全话术，失败时降级为默认值。
func (p *ConfigSafetyRuleProvider) loadMessage(ctx context.Context, msgType, def string) string {
	msgs, err := p.svc.GetSafetyMessages(ctx)
	if err != nil || msgs == nil {
		return def
	}
	switch msgType {
	case entity.SafetyMessageTypeRejection:
		return msgs.RejectionMessage
	case entity.SafetyMessageTypeEmergency:
		return msgs.EmergencyMessage
	case entity.SafetyMessageTypeSafetyWarning:
		return msgs.SafetyWarningMessage
	case entity.SafetyMessageTypeCrisisResponse:
		return msgs.CrisisResponse
	case entity.SafetyMessageTypeNoKnowledge:
		return msgs.NoKnowledgeMessage
	case entity.SafetyMessageTypeSystemError:
		return msgs.SystemErrorMessage
	default:
		return def
	}
}

// ConfigSystemPromptProvider 实现 rag.SystemPromptProvider。
// 从 config 域加载 type='system' 且 is_active=true 的 PromptTemplate（全局/无科室优先）。
// 进程内 5min 缓存（与敏感词/输出规则对齐），避免每消息查 DB + 打 Info 日志。
type ConfigSystemPromptProvider struct {
	svc *configservice.ConfigService

	mu               sync.Mutex
	cachedPrompt     string
	cachedPromptTime time.Time
}

// NewConfigSystemPromptProvider 构造适配器。
func NewConfigSystemPromptProvider(svc *configservice.ConfigService) *ConfigSystemPromptProvider {
	return &ConfigSystemPromptProvider{svc: svc}
}

// GetSystemPrompt 返回当前生效的系统提示词。
// 全局（DepartmentID==nil）优先；其次任一科室级 active 模板。
// 5min 进程内缓存（TTL 与敏感词/输出规则一致）——系统提示词读取频率远高于其变更频率，
// 避免每消息回源 DB。回源失败时降级返回过期缓存，活跃可改变更最多 5min 内生效。
// ponytail: 无主动失效；若需 prompt 变更即时生效，可在 ConfigService 写模板时通知失效，
// 与现有 5min TTL cache-aside 模型对齐，暂不引入通知机制。
func (p *ConfigSystemPromptProvider) GetSystemPrompt(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cachedPrompt != "" && time.Since(p.cachedPromptTime) < sensitiveWordsCacheTTL {
		return p.cachedPrompt, nil
	}
	if content := p.loadSystemPrompt(ctx); content != "" {
		p.cachedPrompt = content
		p.cachedPromptTime = time.Now()
	}
	// 无 active 或回源失败：有过期缓存则降级返回旧值，否则返回空让调用方用默认 prompt（D-HIGH-01）。
	return p.cachedPrompt, nil
}

// loadSystemPrompt 回源 DB 加载当前生效的 system prompt，不存在或出错返回 ""。
func (p *ConfigSystemPromptProvider) loadSystemPrompt(ctx context.Context) string {
	isActive := true
	list, _, err := p.svc.ListPromptTemplates(
		ctx, constants.PromptTypeSystem, &isActive,
		pagination.Params{Page: 1, PageSize: fullScanPageSize},
	)
	if err != nil || len(list) == 0 {
		return ""
	}
	// 全局（DepartmentID == nil）优先；否则取首个科室级 active 模板。
	for _, t := range list {
		if t.DepartmentID == nil {
			return t.Content
		}
	}
	return list[0].Content
}

// 编译期断言。
var _ rag.SafetyRuleProvider = (*ConfigSafetyRuleProvider)(nil)
var _ rag.SystemPromptProvider = (*ConfigSystemPromptProvider)(nil)
