package handler

import (
	"github.com/go-chi/chi/v5"

	"health-nexus/internal/middleware"
)

// RegisterRoutes 在给定 router 上注册 config 域全部 22 个端点。
// 调用方负责在父路由链上挂载 JWTAuth + RequireAdmin 中间件；本处在
// /api/staff/config 路由组内追加 DataIsolation（REQ-SEC-003），
// 执行顺序：JWTAuth → RequireAdmin → DataIsolation。
//
// 权限边界（与前端 superAdminRouteGuard / adminRouteGuard 一致，P1）：
//   - 全局安全配置（AI Provider、敏感词、安全规则、安全话术、审计日志）影响全院，
//     仅超级管理员可读写——科室管理员调用返回 403，不得只靠前端路由限制；
//   - 科室级/运营配置（RAG、Prompt 模板、邀请码等）由科室管理员维护，保持 RequireAdmin。
func (h *ConfigHandler) RegisterRoutes(r chi.Router) {
	r.Route("/api/staff/config", func(r chi.Router) {
		r.Use(middleware.DataIsolation())

		// 配置状态（管理端工作台黄色预警）——概览信息，科室管理员可见。
		r.Get("/status", h.GetConfigStatus)

		// 全局安全配置：仅超级管理员（科室管理员 403）。
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireSuperAdmin())

			// AI Provider（6 个，含连通性测试）
			r.Get("/ai-providers", h.ListAIProviders)
			r.Post("/ai-providers", h.CreateAIProvider)
			r.Get("/ai-providers/{id}", h.GetAIProvider)
			r.Put("/ai-providers/{id}", h.UpdateAIProvider)
			r.Delete("/ai-providers/{id}", h.DeleteAIProvider)
			r.Post("/ai-providers/{id}/test", h.TestAIProvider)

			// 敏感词（4 个）
			r.Get("/sensitive-words", h.ListSensitiveWords)
			r.Post("/sensitive-words", h.CreateSensitiveWord)
			r.Put("/sensitive-words/{id}", h.UpdateSensitiveWord)
			r.Delete("/sensitive-words/{id}", h.DeleteSensitiveWord)

			// 安全规则（4 个）
			r.Get("/safety-rules", h.ListSafetyRules)
			r.Post("/safety-rules", h.CreateSafetyRule)
			r.Put("/safety-rules/{id}", h.UpdateSafetyRule)
			r.Delete("/safety-rules/{id}", h.DeleteSafetyRule)

			// 安全话术（2 个 + policy 只读）
			r.Get("/safety-messages", h.GetSafetyMessages)
			r.Get("/safety-policy", h.GetSafetyPolicy)
			r.Put("/safety-messages", h.UpdateSafetyMessages)

			// 配置审计日志（1 个，REQ-CONFIG-010）
			r.Get("/audit-logs", h.ListAuditLogs)
		})

		// 科室级/运营配置：科室管理员可维护（RequireAdmin）。
		// RAG 配置（2 个）
		r.Get("/rag", h.GetRAGConfig)
		r.Put("/rag", h.UpdateRAGConfig)

		// Prompt 模板（5 个，含 effective 只读查询）
		r.Get("/prompts/effective", h.GetEffectivePrompt)
		r.Get("/prompts", h.ListPromptTemplates)
		r.Post("/prompts", h.CreatePromptTemplate)
		r.Put("/prompts/{id}", h.UpdatePromptTemplate)
		r.Delete("/prompts/{id}", h.DeletePromptTemplate)
	})
}
