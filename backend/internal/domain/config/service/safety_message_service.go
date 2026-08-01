package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"health-nexus/internal/domain/config/entity"
)

// ============ Safety Message ============

// GetSafetyMessages 获取安全话术聚合视图。缺失 type 用默认值兜底（REQ-NFR-016）。
// FIX-6: 单例配置走 cache-aside——先查 Redis，miss 回源 DB 并回填（TTL 5min）。
func (s *ConfigService) GetSafetyMessages(ctx context.Context) (*SafetyMessagesResponse, error) {
	if s.redis != nil {
		if data, err := s.redis.Get(ctx, cacheKeySafetyMessages).Bytes(); err == nil {
			var r SafetyMessagesResponse
			if json.Unmarshal(data, &r) == nil {
				return &r, nil
			}
		}
	}
	msgs, err := s.safetyMessageRepo.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("list safety_messages: %w", err)
	}
	resp := DefaultSafetyMessages
	var latestUpdated time.Time
	for _, m := range msgs {
		applySafetyMessage(&resp, m)
		if m.UpdatedAt.After(latestUpdated) {
			latestUpdated = m.UpdatedAt
		}
	}
	resp.UpdatedAt = latestUpdated
	if s.redis != nil {
		if data, mErr := json.Marshal(resp); mErr == nil {
			s.redis.Set(ctx, cacheKeySafetyMessages, data, cacheTTL)
		}
	}
	return &resp, nil
}

// UpdateSafetyMessages 更新安全话术。仅非 nil 字段更新，其余保持现状。
// FIX-6: 更新成功后失效 Redis 缓存。
// Medium 1: 6 次 Upsert 在同一事务内执行（safetyMessageRepo.Upsert 感知 ctx 内事务），
// 任一失败整体回滚，避免部分写入造成的安全话术不一致。
func (s *ConfigService) UpdateSafetyMessages(
	ctx context.Context, req UpdateSafetyMessagesRequest,
) (*SafetyMessagesResponse, error) {
	updates := []struct {
		msgType string
		content string
	}{
		{entity.SafetyMessageTypeRejection, ptrString(req.RejectionMessage)},
		{entity.SafetyMessageTypeEmergency, ptrString(req.EmergencyMessage)},
		{entity.SafetyMessageTypeSafetyWarning, ptrString(req.SafetyWarningMessage)},
		{entity.SafetyMessageTypeCrisisResponse, ptrString(req.CrisisResponse)},
		{entity.SafetyMessageTypeNoKnowledge, ptrString(req.NoKnowledgeMessage)},
		{entity.SafetyMessageTypeSystemError, ptrString(req.SystemErrorMessage)},
	}
	if err := s.tx.WithTx(ctx, func(ctx context.Context) error {
		for _, u := range updates {
			if u.content == "" {
				continue
			}
			if err := s.safetyMessageRepo.Upsert(ctx, u.msgType, u.content); err != nil {
				return fmt.Errorf("upsert safety_message %s: %w", u.msgType, err)
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	s.invalidateCache(ctx, cacheKeySafetyMessages)
	s.audit(ctx, entity.AuditActionUpdate, entity.AuditEntitySafetyMessage, nil, "fields", len(updates))
	return s.GetSafetyMessages(ctx)
}
