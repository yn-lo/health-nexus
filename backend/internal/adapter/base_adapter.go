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
	repo          *baserepo.DepartmentRepo
	defaultDeptID int64 // chat.default_dept_id；<=0 时取第一个公共启用科室
}

// NewBaseDepartmentResolver 构造适配器。defaultDeptID 来自 chat.default_dept_id（0 = 未配置）。
func NewBaseDepartmentResolver(repo *baserepo.DepartmentRepo, defaultDeptID int64) *BaseDepartmentResolver {
	return &BaseDepartmentResolver{repo: repo, defaultDeptID: defaultDeptID}
}

// ResolveForPatient 解析患者可访问的科室范围。
// selectedDeptID 语义：
//   - nil 或 0：未指定 / 显式选择"全部科室"，两者语义一致——返回零值 Department（不限定科室）；
//     仍校验患者至少有一个可见科室（账号配置异常时不放过），但不落回主科室或首个可见科室。
//   - >0：具体科室，校验其在患者可见列表（所属科室 ∪ 公共科室）中，否则返回 403。
//
// 检索范围由调用方按零值判定为全库（见 chat 域 deptIDPtr：0/nil → 不限科室）。
func (r *BaseDepartmentResolver) ResolveForPatient(
	ctx context.Context, patientID int64, selectedDeptID *int64,
) (rag.Department, error) {
	depts, err := r.repo.ListVisible(ctx, patientID, 0, true)
	if err != nil {
		return rag.Department{}, err
	}
	if len(depts) == 0 {
		return rag.Department{}, apperrors.Forbidden("CHAT_NO_ACCESSIBLE_DEPARTMENT", "患者无可访问科室")
	}
	if selectedDeptID == nil || *selectedDeptID == 0 {
		return rag.Department{}, nil // 全部科室：不限定检索范围
	}
	for _, d := range depts {
		if d.ID == *selectedDeptID {
			return rag.Department{ID: d.ID, Name: d.Name}, nil
		}
	}
	return rag.Department{}, apperrors.Forbidden("CHAT_DEPT_NOT_ACCESSIBLE", "所选科室不可访问")
}

// ResolveForAnonymous 校验匿名用户可选的科室：仅允许公共且启用的科室（ListPublic）。
// selectedDeptID 为 nil 或 0 表示"全部科室"，返回零值 Department（不限定检索范围）。
// 具体 id 不在公共科室列表中（含不存在的 id）返回 403，避免匿名请求携带任意科室 id。
func (r *BaseDepartmentResolver) ResolveForAnonymous(
	ctx context.Context, selectedDeptID *int64,
) (rag.Department, error) {
	if selectedDeptID == nil || *selectedDeptID == 0 {
		return rag.Department{}, nil
	}
	depts, err := r.repo.ListPublic(ctx)
	if err != nil {
		return rag.Department{}, err
	}
	for _, d := range depts {
		if d.ID == *selectedDeptID {
			return rag.Department{ID: d.ID, Name: d.Name}, nil
		}
	}
	return rag.Department{}, apperrors.Forbidden("CHAT_DEPT_NOT_ACCESSIBLE", "所选科室不可访问")
}

// ResolveByID 按科室 id 精确取名（仅名称，不做可访问性校验）。
// 用于会话已锁定、续聊未携带 selected_dept_id 时回填科室名，供统一理解与审查作为语境提示。
// 未找到返回零值 Department（ID=0），不返回错误。
func (r *BaseDepartmentResolver) ResolveByID(ctx context.Context, deptID int64) (rag.Department, error) {
	d, err := r.repo.GetByID(ctx, deptID)
	if err != nil {
		return rag.Department{}, err
	}
	if d == nil {
		return rag.Department{}, nil
	}
	return rag.Department{ID: d.ID, Name: d.Name}, nil
}

// ResolveDefault 返回问答默认责任科室。
// 配置了 chat.default_dept_id（>0）时以其为准（不存在则退化为第一个公共科室）；
// 否则取第一个 is_public 且启用的科室（ListPublic 按 name 排序，生产即"公共科室"）。
// 无可用科室时返回零值 Department（ID=0），不返回错误——由上层决定是否放行。
func (r *BaseDepartmentResolver) ResolveDefault(ctx context.Context) (rag.Department, error) {
	if r.defaultDeptID > 0 {
		d, err := r.repo.GetByID(ctx, r.defaultDeptID)
		if err != nil {
			return rag.Department{}, err
		}
		if d != nil {
			return rag.Department{ID: d.ID, Name: d.Name}, nil
		}
	}
	depts, err := r.repo.ListPublic(ctx)
	if err != nil {
		return rag.Department{}, err
	}
	for _, d := range depts {
		if d.IsActive {
			return rag.Department{ID: d.ID, Name: d.Name}, nil
		}
	}
	return rag.Department{}, nil
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
