// Package adapter 提供跨域适配器，桥接 chat/wiki/base 域的消费者接口。
// 适配器由 DI 层构造并注入，避免跨域直接依赖。
package adapter

import (
	"context"

	baserepo "health-nexus/internal/domain/base/repository"
	wikiservice "health-nexus/internal/domain/wiki/service"
	apperrors "health-nexus/internal/shared/errors"
	"health-nexus/internal/shared/rag"
)

// BaseDepartmentResolver 实现 rag.DepartmentResolver。
// 桥接 base 域的 DepartmentRepo 到 chat 域的消费者接口。
type BaseDepartmentResolver struct {
	repo *baserepo.DepartmentRepo
}

// NewBaseDepartmentResolver 构造适配器。
func NewBaseDepartmentResolver(repo *baserepo.DepartmentRepo) *BaseDepartmentResolver {
	return &BaseDepartmentResolver{repo: repo}
}

// ResolveForPatient 解析患者可访问的科室范围。
// selectedDeptID 语义：
//   - nil：未指定，取患者主科室（未绑定主科室或主科室已禁用时 fallback 到 ListVisible 字母序首个可见科室）；
//   - 0：显式选择"全部科室"，选择本身合法，返回主科室（或首个可见科室）仅作元数据归属，
//     检索范围由 deptIDPtr 控制（0 → nil 不限科室），此处不校验 id=0（数据库中不存在该科室）；
//   - >0：具体科室，校验其在患者可见列表（所属科室 ∪ 公共科室）中，否则返回 403。
// ponytail: selectedDeptID == nil 时优先取患者主科室（user_departments.is_primary），折中。
func (r *BaseDepartmentResolver) ResolveForPatient(
	ctx context.Context, patientID int64, selectedDeptID *int64,
) (rag.Department, error) {
	// "全部科室"(0) 等价于未指定：取主科室作元数据归属，不校验 0 本身。
	if selectedDeptID != nil && *selectedDeptID == 0 {
		selectedDeptID = nil
	}
	if selectedDeptID == nil {
		primary, err := r.repo.GetPrimaryForUser(ctx, patientID)
		if err != nil {
			return rag.Department{}, err
		}
		if primary != nil {
			return rag.Department{ID: primary.ID, Name: primary.Name}, nil
		}
	}
	depts, err := r.repo.ListVisible(ctx, patientID, 0, true)
	if err != nil {
		return rag.Department{}, err
	}
	if len(depts) == 0 {
		return rag.Department{}, apperrors.Forbidden("CHAT_NO_ACCESSIBLE_DEPARTMENT", "患者无可访问科室")
	}
	if selectedDeptID == nil {
		d := depts[0]
		return rag.Department{ID: d.ID, Name: d.Name}, nil
	}
	for _, d := range depts {
		if d.ID == *selectedDeptID {
			return rag.Department{ID: d.ID, Name: d.Name}, nil
		}
	}
	return rag.Department{}, apperrors.Forbidden("CHAT_DEPT_NOT_ACCESSIBLE", "所选科室不可访问")
}

// BaseDepartmentLookup 实现 wiki/service.DepartmentLookup。
// 桥接 base 域到 wiki 域的引用授权校验。
type BaseDepartmentLookup struct {
	repo *baserepo.DepartmentRepo
}

// NewBaseDepartmentLookup 构造适配器。
func NewBaseDepartmentLookup(repo *baserepo.DepartmentRepo) *BaseDepartmentLookup {
	return &BaseDepartmentLookup{repo: repo}
}

// GetByID 返回科室信息（用于引用授权校验 is_public）。
// 使用精确查询而非 ListVisible——后者返回目标科室 ∪ 所有公共科室的并集，
// 按 name 排序后 depts[0] 可能取到字母序最前的公共科室而非 deptID 对应的科室。
func (l *BaseDepartmentLookup) GetByID(ctx context.Context, deptID int64) (*wikiservice.DepartmentInfo, error) {
	d, err := l.repo.GetByID(ctx, deptID)
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, nil
	}
	return &wikiservice.DepartmentInfo{
		ID:       d.ID,
		Name:     d.Name,
		IsPublic: d.IsPublic,
	}, nil
}

// 编译期断言：确保适配器实现接口。
var (
	_ rag.DepartmentResolver       = (*BaseDepartmentResolver)(nil)
	_ wikiservice.DepartmentLookup = (*BaseDepartmentLookup)(nil)
)
