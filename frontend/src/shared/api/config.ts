import { apiClient } from './client';
import type {
  AIProvider,
  AIProviderCreateRequest,
  AIProviderUpdateRequest,
  AIProviderType,
  AIProviderTestResult,
  SensitiveWord,
  SensitiveWordCreateRequest,
  SensitiveWordUpdateRequest,
  SensitiveWordCategory,
  SafetyRule,
  SafetyRuleCreateRequest,
  SafetyRuleUpdateRequest,
  SafetyRuleCategory,
  RAGConfig,
  RAGConfigUpdateRequest,
  SafetyMessages,
  SafetyMessagesUpdateRequest,
  SafetyPolicyResponse,
  PromptTemplate,
  PromptTemplateCreateRequest,
  PromptTemplateUpdateRequest,
  PromptTemplateType,
  EffectivePromptResponse,
  ConfigAuditLog,
  ConfigAuditLogParams,
} from '../types/config';
import type { Paginated } from '../types/base';
import type { SuccessResponse } from '../types/auth';

// ===== 6.1 AI 提供商 =====

/** 获取单个 AI 提供商 */
export function getAIProvider(id: number) {
  return apiClient<AIProvider>(`/staff/config/ai-providers/${id}`);
}

/** 列出 AI 提供商 */
export function listAIProviders(params?: {
  provider_type?: AIProviderType;
  is_active?: boolean;
}) {
  return apiClient<AIProvider[]>('/staff/config/ai-providers', { params });
}

/** 创建 AI 提供商 */
export function createAIProvider(data: AIProviderCreateRequest) {
  return apiClient<AIProvider>('/staff/config/ai-providers', { method: 'POST', body: data });
}

/** 更新 AI 提供商 */
export function updateAIProvider(id: number, data: AIProviderUpdateRequest) {
  return apiClient<AIProvider>(`/staff/config/ai-providers/${id}`, { method: 'PUT', body: data });
}

/** 删除 AI 提供商 */
export function deleteAIProvider(id: number) {
  return apiClient<SuccessResponse>(`/staff/config/ai-providers/${id}`, { method: 'DELETE' });
}

// ===== 6.2 敏感词 =====

/** 列出敏感词（分页） */
export function listSensitiveWords(params?: {
  category?: SensitiveWordCategory;
  page?: number;
  page_size?: number;
}) {
  return apiClient<Paginated<SensitiveWord>>('/staff/config/sensitive-words', { params });
}

/** 创建敏感词 */
export function createSensitiveWord(data: SensitiveWordCreateRequest) {
  return apiClient<SensitiveWord>('/staff/config/sensitive-words', { method: 'POST', body: data });
}

/** 更新敏感词 */
export function updateSensitiveWord(id: number, data: SensitiveWordUpdateRequest) {
  return apiClient<SensitiveWord>(`/staff/config/sensitive-words/${id}`, { method: 'PUT', body: data });
}

/** 删除敏感词 */
export function deleteSensitiveWord(id: number) {
  return apiClient<SuccessResponse>(`/staff/config/sensitive-words/${id}`, { method: 'DELETE' });
}

// ===== 6.3 安全规则 =====

/** 列出安全规则（分页） */
export function listSafetyRules(params?: {
  category?: SafetyRuleCategory;
  page?: number;
  page_size?: number;
}) {
  return apiClient<Paginated<SafetyRule>>('/staff/config/safety-rules', { params });
}

/** 创建安全规则 */
export function createSafetyRule(data: SafetyRuleCreateRequest) {
  return apiClient<SafetyRule>('/staff/config/safety-rules', { method: 'POST', body: data });
}

/** 更新安全规则 */
export function updateSafetyRule(id: number, data: SafetyRuleUpdateRequest) {
  return apiClient<SafetyRule>(`/staff/config/safety-rules/${id}`, { method: 'PUT', body: data });
}

/** 删除安全规则 */
export function deleteSafetyRule(id: number) {
  return apiClient<SuccessResponse>(`/staff/config/safety-rules/${id}`, { method: 'DELETE' });
}

// ===== 6.4 RAG 参数（单例） =====

/** 获取 RAG 配置 */
export function getRAGConfig() {
  return apiClient<RAGConfig>('/staff/config/rag');
}

/** 更新 RAG 配置 */
export function updateRAGConfig(data: RAGConfigUpdateRequest) {
  return apiClient<RAGConfig>('/staff/config/rag', { method: 'PUT', body: data });
}

// ===== 6.5 安全话术（单例聚合） =====

/** 获取安全话术 */
export function getSafetyMessages() {
  return apiClient<SafetyMessages>('/staff/config/safety-messages');
}

/** 更新安全话术 */
export function updateSafetyMessages(data: SafetyMessagesUpdateRequest) {
  return apiClient<SafetyMessages>('/staff/config/safety-messages', { method: 'PUT', body: data });
}

// ===== 6.5.1 安全策略总览 =====

/** 获取安全策略总览（含敏感词、输出规则、话术及来源标注） */
export function getSafetyPolicy() {
  return apiClient<SafetyPolicyResponse>('/staff/config/safety-policy');
}

// ===== 6.6 Prompt 模板 =====

/** 获取当前生效的系统提示词（含硬编码兜底） */
export function getEffectivePrompt() {
  return apiClient<EffectivePromptResponse>('/staff/config/prompts/effective');
}

/** 列出 Prompt 模板（分页） */
export function listPromptTemplates(params?: {
  type?: PromptTemplateType;
  is_active?: boolean;
  page?: number;
  page_size?: number;
}) {
  return apiClient<Paginated<PromptTemplate>>('/staff/config/prompts', { params });
}

/** 创建 Prompt 模板 */
export function createPromptTemplate(data: PromptTemplateCreateRequest) {
  return apiClient<PromptTemplate>('/staff/config/prompts', { method: 'POST', body: data });
}

/** 更新 Prompt 模板 */
export function updatePromptTemplate(id: number, data: PromptTemplateUpdateRequest) {
  return apiClient<PromptTemplate>(`/staff/config/prompts/${id}`, { method: 'PUT', body: data });
}

/** 删除 Prompt 模板 */
export function deletePromptTemplate(id: number) {
  return apiClient<SuccessResponse>(`/staff/config/prompts/${id}`, { method: 'DELETE' });
}

// ===== 6.1.5 AI 提供商连通性测试 =====

/** 测试 AI 提供商连通性 */
export function testAIProvider(id: number) {
  return apiClient<AIProviderTestResult>(`/staff/config/ai-providers/${id}/test`, { method: 'POST' });
}

// ===== 6.7 配置审计日志 =====

/** 查询配置审计日志（分页） */
export function listAuditLogs(params?: ConfigAuditLogParams) {
  return apiClient<Paginated<ConfigAuditLog>>('/staff/config/audit-logs', { params });
}
