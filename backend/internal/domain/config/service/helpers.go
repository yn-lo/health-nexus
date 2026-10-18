package service

import (
	"errors"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"health-nexus/internal/domain/config/entity"
	"health-nexus/internal/domain/config/repository"
	"health-nexus/internal/shared/constants"
	apperrors "health-nexus/internal/shared/errors"
)

const (
	maskedKeyMinLen    = 11
	maskedKeyStarBegin = 3
	maskedKeyStarEnd   = 6

	// 安全规则 pattern / replacement 长度上限（按 rune 计）。
	// Go regexp 使用 RE2 引擎，保证线性时间匹配，无 ReDoS 风险；此处限制是信任边界的
	// 防御性输入校验——DB 列为无界 TEXT，而 pattern 会在每次输出审查（compiledPolicy）
	// 时被重新编译，超大 pattern 会成为资源消耗放大点。500 rune 远超内置 fallback 规则
	// （均 < 100 字符），不影响正常配置。
	safetyRulePatternMaxLen     = 500
	safetyRuleReplacementMaxLen = 500
)

// looksLikeMaskedAPIKey 检测 s 是否为 MaskAPIKey 输出的掩码格式。
// MaskAPIKey 输出：s[:3] + "****" + s[len-4:]，长度 ≥ 11，索引 3-6 为 4 个 '*'。
// 前端把响应中的掩码 API Key 原样回传时，UpdateAIProvider 应跳过加密以保留已存储的密文。
func looksLikeMaskedAPIKey(s string) bool {
	if utf8.RuneCountInString(s) < maskedKeyMinLen {
		return false
	}
	runes := []rune(s)
	for i := maskedKeyStarBegin; i <= maskedKeyStarEnd; i++ {
		if runes[i] != '*' {
			return false
		}
	}
	return true
}

// 子模块允许的取值集合（与 SQL CHECK 约束对齐）。
var (
	providerTypes = []string{
		constants.ProviderTypeLLM,
		constants.ProviderTypeEmbedding,
		constants.ProviderTypeRerank,
	}
	sensitiveCategories = []string{
		constants.SensitiveCategorySuicide,
		constants.SensitiveCategoryEmergency,
		constants.SensitiveCategoryInjection,
	}
	safetyCategories = []string{
		constants.SafetyCategoryDiagnosis,
		constants.SafetyCategoryPrescription,
		constants.SafetyCategoryStopMedication,
		constants.SafetyCategoryDelayMedical,
		constants.SafetyCategoryOther,
	}
	safetyActions = []string{entity.SafetyActionReplace, entity.SafetyActionBlock}
	promptTypes   = []string{
		constants.PromptTypeSystem,
	}
	auditEntityTypes = []string{
		entity.AuditEntityAIProvider,
		entity.AuditEntitySensitiveWord,
		entity.AuditEntitySafetyRule,
		entity.AuditEntityRAGConfig,
		entity.AuditEntitySafetyMessage,
		entity.AuditEntityPromptTemplate,
		entity.AuditEntityBrandConfig,
	}
)

func validateAIProviderFields(providerType, name, apiURL, modelName, apiKey string) error {
	if !slices.Contains(providerTypes, providerType) {
		return apperrors.Validation("CONFIG_INVALID_PROVIDER_TYPE", "provider_type 无效")
	}
	if name == "" {
		return apperrors.Validation("CONFIG_NAME_REQUIRED", "name 不能为空")
	}
	if apiURL == "" {
		return apperrors.Validation("CONFIG_API_URL_REQUIRED", "api_url 不能为空")
	}
	if modelName == "" {
		return apperrors.Validation("CONFIG_MODEL_NAME_REQUIRED", "model_name 不能为空")
	}
	if apiKey == "" {
		return apperrors.Validation("CONFIG_API_KEY_REQUIRED", "api_key 不能为空")
	}
	return nil
}

func validateSafetyRuleFields(name, category, pattern, action, replacement string) error {
	if name == "" {
		return apperrors.Validation("CONFIG_NAME_REQUIRED", "name 不能为空")
	}
	if !slices.Contains(safetyCategories, category) {
		return apperrors.Validation("CONFIG_INVALID_CATEGORY", "category 无效")
	}
	if pattern == "" {
		return apperrors.Validation("CONFIG_PATTERN_REQUIRED", "pattern 不能为空")
	}
	if utf8.RuneCountInString(pattern) > safetyRulePatternMaxLen {
		return apperrors.Validation("CONFIG_PATTERN_TOO_LONG", "pattern 长度不能超过 500 字符")
	}
	if _, err := regexp.Compile(pattern); err != nil {
		return apperrors.Validation("CONFIG_INVALID_PATTERN", "pattern 不是合法正则")
	}
	if !slices.Contains(safetyActions, action) {
		return apperrors.Validation("CONFIG_INVALID_ACTION", "action 无效，必须为 replace 或 block")
	}
	if action == entity.SafetyActionReplace && replacement == "" {
		return apperrors.Validation("CONFIG_REPLACEMENT_REQUIRED", "action=replace 时 replacement 必填")
	}
	if utf8.RuneCountInString(replacement) > safetyRuleReplacementMaxLen {
		return apperrors.Validation("CONFIG_REPLACEMENT_TOO_LONG", "replacement 长度不能超过 500 字符")
	}
	return nil
}

func validateRAGConfig(req UpdateRAGConfigRequest) error {
	if err := checkIntRange(
		req.ChunkSize, entity.ChunkSizeMin, entity.ChunkSizeMax,
		"CONFIG_RAG_CHUNK_SIZE_RANGE", "chunk_size 范围 200-2000",
	); err != nil {
		return err
	}
	if err := checkIntRange(
		req.ChunkOverlap, entity.ChunkOverlapMin, entity.ChunkOverlapMax,
		"CONFIG_RAG_CHUNK_OVERLAP_RANGE", "chunk_overlap 范围 0-500",
	); err != nil {
		return err
	}
	if err := checkIntRange(
		req.MaxChunks, entity.MaxChunksMin, entity.MaxChunksMax,
		"CONFIG_RAG_MAX_CHUNKS_RANGE", "max_chunks 范围 1-50",
	); err != nil {
		return err
	}
	if err := checkIntRange(
		req.TopK, entity.TopKMin, entity.TopKMax,
		"CONFIG_RAG_TOP_K_RANGE", "top_k 范围 1-50",
	); err != nil {
		return err
	}
	if err := checkFloatRange(
		req.SimilarityThreshold, entity.SimilarityThresholdMax,
		"CONFIG_RAG_SIMILARITY_RANGE", "similarity_threshold 范围 0.0-1.0",
	); err != nil {
		return err
	}
	if err := checkFloatRange(
		req.RerankThreshold, entity.RerankThresholdMax,
		"CONFIG_RAG_RERANK_THRESHOLD_RANGE", "rerank_threshold 范围 0.0-1.0",
	); err != nil {
		return err
	}
	return nil
}

func checkIntRange(v *int, lo, hi int, code, msg string) error {
	if v != nil && (*v < lo || *v > hi) {
		return apperrors.Validation(code, msg)
	}
	return nil
}

func checkFloatRange(v *float64, hi float64, code, msg string) error {
	if v != nil && (*v < 0 || *v > hi) {
		return apperrors.Validation(code, msg)
	}
	return nil
}

func applyRAGPatch(c *entity.RAGConfig, req UpdateRAGConfigRequest) {
	if req.ChunkSize != nil {
		c.ChunkSize = *req.ChunkSize
	}
	if req.ChunkOverlap != nil {
		c.ChunkOverlap = *req.ChunkOverlap
	}
	if req.MaxChunks != nil {
		c.MaxChunks = *req.MaxChunks
	}
	if req.TopK != nil {
		c.TopK = *req.TopK
	}
	if req.SimilarityThreshold != nil {
		c.SimilarityThreshold = *req.SimilarityThreshold
	}
	if req.RerankEnabled != nil {
		c.RerankEnabled = *req.RerankEnabled
	}
	if req.RerankThreshold != nil {
		c.RerankThreshold = *req.RerankThreshold
	}
}

func validateBrandConfig(req UpdateBrandConfigRequest) error {
	if err := checkTextLen(
		req.Name, entity.BrandNameMaxLen,
		"CONFIG_BRAND_NAME_TOO_LONG", "name 长度不能超过 50 字符",
	); err != nil {
		return err
	}
	if err := checkTextLen(
		req.Wordmark, entity.BrandWordmarkMaxLen,
		"CONFIG_BRAND_WORDMARK_TOO_LONG", "wordmark 长度不能超过 50 字符",
	); err != nil {
		return err
	}
	if err := checkTextLen(
		req.OrgName, entity.BrandOrgNameMaxLen,
		"CONFIG_BRAND_ORG_NAME_TOO_LONG", "org_name 长度不能超过 100 字符",
	); err != nil {
		return err
	}
	if err := checkTextLen(
		req.Slogan, entity.BrandSloganMaxLen,
		"CONFIG_BRAND_SLOGAN_TOO_LONG", "slogan 长度不能超过 50 字符",
	); err != nil {
		return err
	}
	if err := checkAbsoluteURL(
		req.SiteURL, "CONFIG_BRAND_SITE_URL_INVALID", "site_url 必须为 http(s) 绝对地址",
	); err != nil {
		return err
	}
	if err := checkTextLen(
		req.SiteTitle, entity.BrandSiteTitleMaxLen,
		"CONFIG_BRAND_SITE_TITLE_TOO_LONG", "site_title 长度不能超过 100 字符",
	); err != nil {
		return err
	}
	if err := checkBrandURL(
		req.LogoURL, "CONFIG_BRAND_LOGO_URL_INVALID", "logo_url 必须为站内路径或 http(s) 地址",
	); err != nil {
		return err
	}
	return checkBrandURL(
		req.FaviconURL, "CONFIG_BRAND_FAVICON_URL_INVALID", "favicon_url 必须为站内路径或 http(s) 地址",
	)
}

func applyBrandPatch(c *entity.BrandConfig, req UpdateBrandConfigRequest) {
	if req.Name != nil {
		c.Name = strings.TrimSpace(*req.Name)
	}
	if req.Wordmark != nil {
		c.Wordmark = strings.TrimSpace(*req.Wordmark)
	}
	if req.OrgName != nil {
		c.OrgName = strings.TrimSpace(*req.OrgName)
	}
	if req.Slogan != nil {
		c.Slogan = strings.TrimSpace(*req.Slogan)
	}
	if req.SiteURL != nil {
		c.SiteURL = strings.TrimSpace(*req.SiteURL)
	}
	if req.LogoURL != nil {
		c.LogoURL = strings.TrimSpace(*req.LogoURL)
	}
	if req.SiteTitle != nil {
		c.SiteTitle = strings.TrimSpace(*req.SiteTitle)
	}
	if req.FaviconURL != nil {
		c.FaviconURL = strings.TrimSpace(*req.FaviconURL)
	}
}

// checkTextLen 校验可选文本字段长度（按 rune 计）。空串允许（表示清空）。
func checkTextLen(v *string, maxLen int, code, msg string) error {
	if v != nil && utf8.RuneCountInString(*v) > maxLen {
		return apperrors.Validation(code, msg)
	}
	return nil
}

// checkBrandURL 校验可选 URL 字段：空串允许；否则须为站内相对路径或 http(s) 绝对地址。
// 拒绝 javascript:/data: 等伪协议——前端会把该值直接写入 <img src> / <link href>。
func checkBrandURL(v *string, code, msg string) error {
	if v == nil || *v == "" {
		return nil
	}
	u := *v
	if utf8.RuneCountInString(u) > entity.BrandURLMaxLen {
		return apperrors.Validation(code, msg)
	}
	if strings.HasPrefix(u, "/") ||
		strings.HasPrefix(u, "http://") ||
		strings.HasPrefix(u, "https://") {
		return nil
	}
	return apperrors.Validation(code, msg)
}

// checkAbsoluteURL 校验可选外链字段：空串允许；否则须为 http(s) 绝对地址（外链用相对路径无意义）。
func checkAbsoluteURL(v *string, code, msg string) error {
	if v == nil || *v == "" {
		return nil
	}
	u := *v
	if utf8.RuneCountInString(u) > entity.BrandURLMaxLen {
		return apperrors.Validation(code, msg)
	}
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return nil
	}
	return apperrors.Validation(code, msg)
}

func ptrString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// translateRepoErr 把 repo 的 ErrNotFound 翻译为 AppError 404；其他错误原样返回。
func translateRepoErr(err error, notFoundCode, notFoundMsg string) error {
	if errors.Is(err, repository.ErrNotFound) {
		return apperrors.NotFound(notFoundCode, notFoundMsg)
	}
	return err
}
