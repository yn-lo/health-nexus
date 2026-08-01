/**
 * config 域类型 — 对齐后端 internal/domain/config/entity/* 与 api-contracts.md §6
 */

/** AI 提供商类型 */
export type AIProviderType = 'llm' | 'embedding' | 'rerank' | 'rewrite';

/** AI 提供商响应（api_key 返回掩码） */
export interface AIProvider {
  id: number;
  name: string;
  provider_type: AIProviderType;
  api_base: string;
  api_key: string;
  model_name: string;
  dimension: number | null;
  params: Record<string, unknown>;
  is_active: boolean;
  is_full_url: boolean;
  created_at: string;
  updated_at: string;
}

/** 创建 AI 提供商请求 */
export interface AIProviderCreateRequest {
  provider_type: AIProviderType;
  name: string;
  api_base: string;
  api_key: string;
  model_name: string;
  dimension?: number;
  params?: Record<string, unknown>;
  is_active?: boolean;
  is_full_url?: boolean;
}

/** 更新 AI 提供商请求（所有字段可选） */
export interface AIProviderUpdateRequest {
  name?: string;
  api_base?: string;
  api_key?: string;
  model_name?: string;
  dimension?: number;
  params?: Record<string, unknown>;
  is_active?: boolean;
  is_full_url?: boolean;
}

/** 敏感词类别 */
export type SensitiveWordCategory = 'suicide' | 'emergency' | 'injection';

/** 敏感词响应 */
export interface SensitiveWord {
  id: number;
  word: string;
  category: SensitiveWordCategory;
  is_active: boolean;
  created_at: string;
}

/** 创建敏感词请求 */
export interface SensitiveWordCreateRequest {
  word: string;
  category: SensitiveWordCategory;
  is_active?: boolean;
}

/** 更新敏感词请求 */
export interface SensitiveWordUpdateRequest {
  word?: string;
  category?: SensitiveWordCategory;
  is_active?: boolean;
}

/** 安全规则类别 */
export type SafetyRuleCategory =
  | 'diagnosis'
  | 'prescription'
  | 'stop_medication'
  | 'delay_medical'
  | 'other';

/** 安全规则动作 */
export type SafetyRuleAction = 'replace' | 'block';

/** 安全规则响应 */
export interface SafetyRule {
  id: number;
  name: string;
  category: SafetyRuleCategory;
  pattern: string;
  action: SafetyRuleAction;
  replacement: string;
  is_active: boolean;
  description: string;
  created_at: string;
  updated_at: string;
}

/** 创建安全规则请求 */
export interface SafetyRuleCreateRequest {
  name: string;
  category: SafetyRuleCategory;
  pattern: string;
  action: SafetyRuleAction;
  replacement?: string;
  is_active?: boolean;
  description?: string;
}

/** 更新安全规则请求 */
export interface SafetyRuleUpdateRequest {
  name?: string;
  category?: SafetyRuleCategory;
  pattern?: string;
  action?: SafetyRuleAction;
  replacement?: string;
  is_active?: boolean;
  description?: string;
}

/** RAG 配置响应（单例） */
export interface RAGConfig {
  chunk_size: number;
  chunk_overlap: number;
  max_chunks: number;
  top_k: number;
  similarity_threshold: number;
  rerank_enabled: boolean;
  rerank_threshold: number;
  diversity_factor: number;
  ood_threshold: number;
  updated_at: string;
}

/** 更新 RAG 配置请求 */
export interface RAGConfigUpdateRequest {
  chunk_size?: number;
  chunk_overlap?: number;
  max_chunks?: number;
  top_k?: number;
  similarity_threshold?: number;
  rerank_enabled?: boolean;
  rerank_threshold?: number;
  diversity_factor?: number;
  ood_threshold?: number;
}

/** RAG 参数范围（与后端 entity.go 一致，用于前端校验提示） */
export const RAG_LIMITS = {
  chunk_size: { min: 200, max: 2000 },
  chunk_overlap: { min: 0, max: 500 },
  max_chunks: { min: 1, max: 50 },
  top_k: { min: 1, max: 50 },
  similarity_threshold: { min: 0, max: 1 },
  rerank_threshold: { min: 0, max: 1 },
  diversity_factor: { min: 0, max: 1 },
  ood_threshold: { min: 0, max: 0.5 },
} as const;

/** 安全话术响应（聚合单例，crisis_hotline 已合并到 crisis_response，medication_disclaimer 已合并到 safety_warning_message） */
export interface SafetyMessages {
  rejection_message: string;
  emergency_message: string;
  safety_warning_message: string;
  crisis_response: string;
  no_knowledge_message: string;
  system_error_message: string;
  updated_at: string;
}

/** 更新安全话术请求 */
export interface SafetyMessagesUpdateRequest {
  rejection_message?: string;
  emergency_message?: string;
  safety_warning_message?: string;
  crisis_response?: string;
  no_knowledge_message?: string;
  system_error_message?: string;
}

/** Prompt 模板类型。仅 system 类型在运行时被注入 LLM；安全话术由 safety_messages 管理。 */
export type PromptTemplateType = 'system';

/** Prompt 模板响应 */
export interface PromptTemplate {
  id: number;
  type: PromptTemplateType;
  version: number;
  content: string;
  is_active: boolean;
  description: string;
  department_id: number | null;
  created_at: string;
  updated_at: string;
}

/** 创建 Prompt 模板请求 */
export interface PromptTemplateCreateRequest {
  type: PromptTemplateType;
  content: string;
  is_active?: boolean;
  description?: string;
  department_id?: number | null;
}

/** 更新 Prompt 模板请求 */
export interface PromptTemplateUpdateRequest {
  content?: string;
  is_active?: boolean;
}

/** 当前生效的系统提示词响应 */
export interface EffectivePromptResponse {
  content: string;
  source: 'database' | 'default';
}

// ===== 6.1.5 AI 提供商连通性测试结果 =====

/** AI 提供商连通性测试结果 */
export interface AIProviderTestResult {
  success: boolean;
  latency_ms: number;
  detail: string;
}

// ===== 6.7 配置审计日志 =====

/** 配置审计日志实体类型 */
export type ConfigAuditEntityType =
  | 'ai_provider'
  | 'sensitive_word'
  | 'safety_rule'
  | 'rag_config'
  | 'safety_message'
  | 'prompt_template';

/** 配置审计日志响应 */
export interface ConfigAuditLog {
  id: number;
  action: string;
  entity_type: ConfigAuditEntityType;
  entity_id: number | null;
  operator_id: number;
  operator_role: string;
  changes: Record<string, unknown> | null;
  created_at: string;
}

/** 安全策略总览 — 敏感词来源词集 */
export interface SafetyPolicyWords {
  source: 'default' | 'database';
  words: string[];
}

/** 安全策略总览 — 输入敏感词（按类别） */
export interface SafetyPolicyInputWords {
  suicide: SafetyPolicyWords;
  emergency: SafetyPolicyWords;
  injection: SafetyPolicyWords;
}

/** 安全策略总览 — 输出安全规则（含来源标注） */
export interface SafetyPolicyOutputRule {
  category: string;
  pattern: string;
  action: string;
  replacement: string;
  source: 'database' | 'hardcoded';
}

/** 安全策略总览响应 */
export interface SafetyPolicyResponse {
  input_sensitive_words: SafetyPolicyInputWords;
  output_rules: SafetyPolicyOutputRule[];
  messages: SafetyMessages;
}

/** 配置审计日志查询参数 */
export interface ConfigAuditLogParams {
  entity_type?: ConfigAuditEntityType;
  entity_id?: number;
  page?: number;
  page_size?: number;
}
