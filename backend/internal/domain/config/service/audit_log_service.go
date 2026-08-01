package service

import (
	"context"
	"fmt"
	"slices"

	apperrors "health-nexus/internal/shared/errors"
	"health-nexus/internal/shared/pagination"
)

// ============ Audit Log ============

// ListAuditLogs 列出配置变更审计日志，可选 entity_type / entity_id 过滤，分页。
// entityID 为 0 时按 entity_id IS NULL 过滤（单例配置审计记录）。
// ponytail: 不做角色级数据隔离——config 域已统一 RequireAdmin，DEPT_ADMIN 可见全部审计记录，折中。
// 上限：跨科室变更可见；升级路径：在表上加 department_id 列 + 中间件注入 dept 过滤。
func (s *ConfigService) ListAuditLogs(
	ctx context.Context, entityType string, entityID int64, p pagination.Params,
) ([]ConfigAuditLogResponse, int64, error) {
	if entityType != "" && !slices.Contains(auditEntityTypes, entityType) {
		return nil, 0, apperrors.Validation("CONFIG_INVALID_ENTITY_TYPE", "entity_type 无效")
	}
	items, total, err := s.auditLogRepo.ListByEntity(ctx, entityType, entityID, p.Page, p.PageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list config_audit_logs: %w", err)
	}
	out := make([]ConfigAuditLogResponse, 0, len(items))
	for _, l := range items {
		out = append(out, toConfigAuditLogResponse(l))
	}
	return out, int64(total), nil
}
