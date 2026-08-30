// 类型导出
export type {
  LoginRequest,
  RegisterRequest,
  TokenResponse,
  TokenUser,
  SuccessResponse,
  PasswordResetRequestRequest,
  PasswordResetConfirmRequest,
  ChangePasswordRequest,
  UpdateProfileRequest,
  StaffAccount,
  StaffAccountCreateRequest,
  ResetPasswordRequest,
  InviteCode,
} from './types/auth';
export type { Conversation, ConversationUpdateRequest, ConversationListParams, Message, MessageListParams, Reference, SSEEvent } from './types/chat';
export type { ArticleStatus, ArticlePublic, ArticleDetail, ArticleStaff, ArticleChunk, ArticleCreateRequest, ArticleUpdateRequest, ArticleListParams, ArticleStaffListParams, ArticleReference, ReferenceApplyRequest, ReferenceListParams, ReferenceStatus } from './types/wiki';
export type { Department, DepartmentTreeNode, DepartmentCreateRequest, DepartmentUpdateRequest, Paginated } from './types/base';
export type { CrisisEventItem, CrisisLevel, CrisisEventListParams, CrisisEventHandleRequest } from './types/staffChat';
export type { MenuItem } from './types/menu';
export type {
  AIProvider, AIProviderType, AIProviderCreateRequest, AIProviderUpdateRequest, AIProviderTestResult,
  SensitiveWord, SensitiveWordCategory, SensitiveWordCreateRequest, SensitiveWordUpdateRequest,
  SafetyRule, SafetyRuleCategory, SafetyRuleAction, SafetyRuleCreateRequest, SafetyRuleUpdateRequest,
  RAGConfig, RAGConfigUpdateRequest,
  SafetyMessages, SafetyMessagesUpdateRequest,
  SafetyPolicyWords, SafetyPolicyInputWords, SafetyPolicyOutputRule, SafetyPolicyResponse,
  PromptTemplate, PromptTemplateType, PromptTemplateCreateRequest, PromptTemplateUpdateRequest,
  EffectivePromptResponse,
  ConfigAuditEntityType, ConfigAuditLog, ConfigAuditLogParams,
} from './types/config';
export { RAG_LIMITS } from './types/config';

// API client
export { apiClient, getAccessToken, getRefreshToken, getDeviceId, setTokens, clearTokens, getUserStored, setUserStored, errmsg } from './api/client';

// API 模块
export * as authApi from './api/auth';
export * as chatApi from './api/chat';
export * as wikiApi from './api/wiki';
export * as baseApi from './api/base';
export * as configApi from './api/config';
export * as staffChatApi from './api/staffChat';

// Composables
export {
  useDepartmentOptions,
  DEPARTMENT_GLOBAL,
} from './composables/useDepartmentOptions';

export { useDsToast } from './composables/useDsToast';
export { useDsDialog } from './composables/useDsDialog';
export { useProfileSummary } from './composables/useProfileSummary';
export { usePagedList } from './composables/usePagedList';
export { useCrudEditor } from './composables/useCrudEditor';

// Utils
export { timeAgo, fmtDate, fmtDateTime, fmtShortDate, fmtCompact, fmtUserId, stripHtml } from './utils/format';
