// Package service 实现 base 域的业务编排（事务边界 + 领域规则）。
package service

import (
	"context"
	"fmt"
	"slices"
	"time"

	"health-nexus/internal/domain/base/entity"
	"health-nexus/internal/middleware"
	"health-nexus/internal/platform/postgres"
	"health-nexus/internal/shared/constants"
	apperrors "health-nexus/internal/shared/errors"
)

// DepartmentRepo 是 base 域需要的科室持久化能力（消费者定义，ISP）。
// 由 repository 包实现，通过 InitializeApp 注入。
type DepartmentRepo interface {
	ListVisible(ctx context.Context, userID, deptID int64, active bool) ([]*entity.Department, error)
	ListPublic(ctx context.Context) ([]*entity.Department, error)
	GetPrimaryForUser(ctx context.Context, userID int64) (*entity.Department, error)
	GetByID(ctx context.Context, id int64) (*entity.Department, error)
	ListAll(ctx context.Context) ([]*entity.Department, error)
	ListSubtree(ctx context.Context, rootID int64) ([]*entity.Department, error)
	ListDescendantIDs(ctx context.Context, rootID int64) ([]int64, error)
	HasChildren(ctx context.Context, id int64) (bool, error)
	HasUsers(ctx context.Context, id int64) (bool, error)
	SiblingNameExists(ctx context.Context, name string, parentID *int64, excludeID int64) (bool, error)
	Create(ctx context.Context, d *entity.Department) error
	UpdateFields(ctx context.Context, id int64, fields map[string]any) (*entity.Department, error)
	Delete(ctx context.Context, id int64) error
}

// departmentNameMax 名称长度上限（与 schema VARCHAR(100) 对齐）。
const departmentNameMax = 100

// DepartmentDTO 科室响应 DTO（对应 API 契约 DepartmentResponse）。
// 不含 description——ListVisible 公开接口未暴露该字段，避免泄露内部备注。
type DepartmentDTO struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	ParentID  *int64    `json:"parent_id"` // 根科室为 null
	IsPublic  bool      `json:"is_public"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

// DepartmentTreeDTO 管理员视图 DTO（含 description + updated_at，契约 §2.2-2.6）。
type DepartmentTreeDTO struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	ParentID    *int64    `json:"parent_id"`
	IsPublic    bool      `json:"is_public"`
	IsActive    bool      `json:"is_active"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Actor 操作者上下文（由 handler 从 JWT ctx 提取后传入；与 wiki 域 Actor 一致风格）。
type Actor struct {
	UserID int64
	Role   string
	DeptID int64
}

// ActorFromDataScope 从 ctx 中的 DataScope（DataIsolation 中间件注入）构造 Actor。
// 返回 ok=false 表示 ctx 未挂载 DataScope（如未走 DataIsolation 中间件的路由）。
func ActorFromDataScope(ctx context.Context) (Actor, bool) {
	scope := middleware.ScopeFromCtx(ctx)
	if scope == nil {
		return Actor{}, false
	}
	return Actor{UserID: scope.UserID, Role: scope.Role, DeptID: scope.DeptID}, true
}

// DepartmentService 科室业务服务：只读可见列表 + 管理员 CRUD + 树形/环路校验。
type DepartmentService struct {
	repo DepartmentRepo
	tx   *postgres.TxManager
}

// NewDepartmentService 构造科室服务。tx 为 nil 时 CRUD 方法以非事务模式运行
// （仅用于单元测试场景，生产环境必须注入 TxManager）。
func NewDepartmentService(repo DepartmentRepo, tx *postgres.TxManager) *DepartmentService {
	return &DepartmentService{repo: repo, tx: tx}
}

// runTx 在事务中执行 fn；tx 为 nil 时直接执行（单元测试场景）。
// 生产路径必须注入 TxManager 以保证 ACID（service 是唯一事务边界，AC-ARCH-03）。
func (s *DepartmentService) runTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if s.tx == nil {
		return fn(ctx)
	}
	return s.tx.WithTx(ctx, fn)
}

// ListVisible 返回当前用户可见的科室列表（REQ-BASE-004）。
// userID/deptID 来自 JWT，active 过滤启用状态。
func (s *DepartmentService) ListVisible(
	ctx context.Context, userID, deptID int64, active bool,
) ([]DepartmentDTO, error) {
	depts, err := s.repo.ListVisible(ctx, userID, deptID, active)
	if err != nil {
		return nil, fmt.Errorf("list visible departments: %w", err)
	}
	dtos := make([]DepartmentDTO, 0, len(depts))
	for _, d := range depts {
		dtos = append(dtos, toDepartmentDTO(d))
	}
	return dtos, nil
}

// ListPublic 返回所有公共且启用的科室列表（REQ-BASE-013）。
// 无需认证，供匿名用户选择咨询科室。
func (s *DepartmentService) ListPublic(ctx context.Context) ([]DepartmentDTO, error) {
	depts, err := s.repo.ListPublic(ctx)
	if err != nil {
		return nil, fmt.Errorf("list public departments: %w", err)
	}
	dtos := make([]DepartmentDTO, 0, len(depts))
	for _, d := range depts {
		dtos = append(dtos, toDepartmentDTO(d))
	}
	return dtos, nil
}

// ============ Staff CRUD ============

// CreateDeptInput 创建科室输入（REQ-BASE-005）。
// ParentID 为 nil 表示根科室；为 *0 也视为根科室（handler JSON 无法直接传 null 时用 0 兜底）。
type CreateDeptInput struct {
	Name        string
	ParentID    *int64
	IsPublic    bool
	IsActive    bool
	Description string
	Actor       Actor
}

// Create 创建科室（REQ-BASE-005/011）。事务内：parent_id 校验 + 子树范围校验 + INSERT。
func (s *DepartmentService) Create(ctx context.Context, in CreateDeptInput) (*DepartmentTreeDTO, error) {
	if in.Name == "" {
		return nil, apperrors.Validation("BASE_DEPT_NAME_REQUIRED", "name 不能为空")
	}
	if len(in.Name) > departmentNameMax {
		return nil, apperrors.Validation("BASE_DEPT_NAME_TOO_LONG", "name 长度需为 1-100 字符")
	}

	// 归一化 ParentID：*0 视为根科室（handler JSON null 不好表达时的兜底）
	parentID := normalizeParentID(in.ParentID)

	if parentID != nil {
		parent, err := s.repo.GetByID(ctx, *parentID)
		if err != nil {
			return nil, fmt.Errorf("get parent department: %w", err)
		}
		if parent == nil {
			return nil, apperrors.BadRequest("BASE_DEPT_PARENT_NOT_FOUND", "父科室不存在")
		}
	}

	// DEPT_ADMIN 子树范围收口（REQ-BASE-011）：parent 必须在自身子树内（含自身）
	if err := s.assertInScope(ctx, in.Actor, parentID); err != nil {
		return nil, err
	}

	if err := s.assertNameUnique(ctx, in.Name, parentID, 0); err != nil {
		return nil, err
	}

	d := &entity.Department{
		Name:        in.Name,
		ParentID:    parentID,
		IsPublic:    in.IsPublic,
		IsActive:    in.IsActive,
		Description: in.Description,
	}

	if err := s.runTx(ctx, func(ctx context.Context) error {
		return s.repo.Create(ctx, d)
	}); err != nil {
		return nil, fmt.Errorf("create department: %w", err)
	}

	dto := toTreeDTO(d)
	return &dto, nil
}

// ListTree 管理员视图：SUPER_ADMIN 全树，DEPT_ADMIN 仅主科室子树（REQ-BASE-006/011）。
func (s *DepartmentService) ListTree(ctx context.Context, actor Actor) ([]DepartmentTreeDTO, error) {
	var depts []*entity.Department
	var err error
	if actor.Role == constants.RoleSuperAdmin {
		depts, err = s.repo.ListAll(ctx)
	} else {
		depts, err = s.repo.ListSubtree(ctx, actor.DeptID)
	}
	if err != nil {
		return nil, fmt.Errorf("list tree: %w", err)
	}
	dtos := make([]DepartmentTreeDTO, 0, len(depts))
	for _, d := range depts {
		dtos = append(dtos, toTreeDTO(d))
	}
	return dtos, nil
}

// Get 获取单个科室详情（REQ-BASE-007/011）。
// 顺序：先范围收口（避免向 DEPT_ADMIN 泄露科室存在性），再存在性查询。
func (s *DepartmentService) Get(ctx context.Context, id int64, actor Actor) (*DepartmentTreeDTO, error) {
	if err := s.assertInScope(ctx, actor, &id); err != nil {
		return nil, err
	}
	d, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get department: %w", err)
	}
	if d == nil {
		return nil, apperrors.NotFound("BASE_DEPT_NOT_FOUND", "科室不存在")
	}
	dto := toTreeDTO(d)
	return &dto, nil
}

// UpdateDeptInput 更新科室输入（REQ-BASE-008）。全字段可选，nil 表示不更新。
// ParentID 特殊：*0 表示变为根科室，nil 表示不更新 parent_id。
type UpdateDeptInput struct {
	Name        *string
	Description *string
	IsPublic    *bool
	IsActive    *bool
	ParentID    *int64 // *0 = 变根科室；nil = 不动；*N = 移到 N 下
	Actor       Actor
}

// Update 更新科室（REQ-BASE-008/009/011）。事务内：范围 + 存在性 + 环路 + 父存在性校验 + UPDATE。
// 顺序：先范围收口（避免向 DEPT_ADMIN 泄露科室存在性），再存在性查询。
func (s *DepartmentService) Update(
	ctx context.Context, id int64, in UpdateDeptInput,
) (*DepartmentTreeDTO, error) {
	if err := validateUpdateInput(in); err != nil {
		return nil, err
	}
	if err := s.assertInScope(ctx, in.Actor, &id); err != nil {
		return nil, err
	}
	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get department: %w", err)
	}
	if existing == nil {
		return nil, apperrors.NotFound("BASE_DEPT_NOT_FOUND", "科室不存在")
	}
	var newParentID *int64
	if in.ParentID != nil {
		newParentID, err = s.validateParentChange(ctx, id, in)
		if err != nil {
			return nil, err
		}
	}
	if in.Name != nil {
		effectiveParent := existing.ParentID
		if newParentID != nil {
			effectiveParent = newParentID
		}
		if err := s.assertNameUnique(ctx, *in.Name, effectiveParent, id); err != nil {
			return nil, err
		}
	}
	fields := buildUpdateFields(in, newParentID)
	var updated *entity.Department
	if err := s.runTx(ctx, func(ctx context.Context) error {
		var err error
		updated, err = s.repo.UpdateFields(ctx, id, fields)
		return err
	}); err != nil {
		return nil, fmt.Errorf("update department: %w", err)
	}
	if updated == nil {
		return nil, apperrors.NotFound("BASE_DEPT_NOT_FOUND", "科室不存在")
	}
	dto := toTreeDTO(updated)
	return &dto, nil
}

func validateUpdateInput(in UpdateDeptInput) error {
	if in.Name == nil && in.Description == nil && in.IsPublic == nil && in.IsActive == nil && in.ParentID == nil {
		return apperrors.Validation(
			"BASE_DEPT_EMPTY_UPDATE", "至少需要一个字段：name/description/is_public/is_active/parent_id")
	}
	if in.Name != nil && len(*in.Name) > departmentNameMax {
		return apperrors.Validation("BASE_DEPT_NAME_TOO_LONG", "name 长度需为 1-100 字符")
	}
	return nil
}

func (s *DepartmentService) validateParentChange(
	ctx context.Context, id int64, in UpdateDeptInput,
) (*int64, error) {
	newParentID := normalizeParentID(in.ParentID)
	if newParentID == nil {
		return nil, nil
	}
	if *newParentID == id {
		return nil, apperrors.Conflict("BASE_DEPT_CYCLE", "不能将科室移动到自身或其子科室下")
	}
	parent, err := s.repo.GetByID(ctx, *newParentID)
	if err != nil {
		return nil, fmt.Errorf("get parent department: %w", err)
	}
	if parent == nil {
		return nil, apperrors.BadRequest("BASE_DEPT_PARENT_NOT_FOUND", "父科室不存在")
	}
	descendants, err := s.repo.ListDescendantIDs(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list descendants for cycle check: %w", err)
	}
	if slices.Contains(descendants, *newParentID) {
		return nil, apperrors.Conflict("BASE_DEPT_CYCLE", "不能将科室移动到自身或其子科室下")
	}
	if err := s.assertInScope(ctx, in.Actor, newParentID); err != nil {
		return nil, err
	}
	return newParentID, nil
}

func buildUpdateFields(in UpdateDeptInput, newParentID *int64) map[string]any {
	fields := make(map[string]any, 5)
	if in.Name != nil {
		fields["name"] = *in.Name
	}
	if in.Description != nil {
		fields["description"] = *in.Description
	}
	if in.IsPublic != nil {
		fields["is_public"] = *in.IsPublic
	}
	if in.IsActive != nil {
		fields["is_active"] = *in.IsActive
	}
	if in.ParentID != nil {
		fields["parent_id"] = newParentID
	}
	return fields
}

// Delete 删除科室（REQ-BASE-010/011）。
// 前置校验：范围 + 存在性 + 无子科室 + 无关联用户。
// 顺序：先范围收口（避免向 DEPT_ADMIN 泄露科室存在性），再存在性查询。
func (s *DepartmentService) Delete(ctx context.Context, id int64, actor Actor) (bool, error) {
	if err := s.assertInScope(ctx, actor, &id); err != nil {
		return false, err
	}
	d, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return false, fmt.Errorf("get department: %w", err)
	}
	if d == nil {
		return false, apperrors.NotFound("BASE_DEPT_NOT_FOUND", "科室不存在")
	}

	hasChildren, err := s.repo.HasChildren(ctx, id)
	if err != nil {
		return false, fmt.Errorf("check children: %w", err)
	}
	if hasChildren {
		return false, apperrors.Conflict("BASE_DEPT_HAS_CHILDREN", "该科室仍有子科室，请先迁移或删除子科室")
	}
	hasUsers, err := s.repo.HasUsers(ctx, id)
	if err != nil {
		return false, fmt.Errorf("check users: %w", err)
	}
	if hasUsers {
		return false, apperrors.Conflict("BASE_DEPT_HAS_USERS", "该科室仍有用户关联，请先迁移用户")
	}

	if err := s.runTx(ctx, func(ctx context.Context) error {
		return s.repo.Delete(ctx, id)
	}); err != nil {
		return false, fmt.Errorf("delete department: %w", err)
	}
	return true, nil
}

// assertInScope 校验 targetDeptID（可为 nil=根科室，对 DEPT_ADMIN 而言根科室永远 out of scope）
// 是否在 actor 的可管理范围内。SUPER_ADMIN 总是通过；DEPT_ADMIN 必须在 {主科室} ∪ descendants 内。
func (s *DepartmentService) assertInScope(ctx context.Context, actor Actor, targetDeptID *int64) error {
	if actor.Role == constants.RoleSuperAdmin {
		return nil
	}
	if actor.Role != constants.RoleDeptAdmin {
		// DOCTOR/NURSE 不应到达此路径（RequireAdmin 中间件已拦截），防御性返回 403
		return apperrors.Forbidden("BASE_DEPT_OUT_OF_SCOPE", "仅可管理本科室子树内的科室")
	}
	if targetDeptID == nil {
		// DEPT_ADMIN 不能在根级创建/移动到根
		return apperrors.Forbidden("BASE_DEPT_OUT_OF_SCOPE", "仅可管理本科室子树内的科室")
	}
	if *targetDeptID == actor.DeptID {
		return nil // 自身
	}
	descendants, err := s.repo.ListDescendantIDs(ctx, actor.DeptID)
	if err != nil {
		return fmt.Errorf("list descendants for scope check: %w", err)
	}
	if !slices.Contains(descendants, *targetDeptID) {
		return apperrors.Forbidden("BASE_DEPT_OUT_OF_SCOPE", "仅可管理本科室子树内的科室")
	}
	return nil
}

func (s *DepartmentService) assertNameUnique(ctx context.Context, name string, parentID *int64, excludeID int64) error {
	exists, err := s.repo.SiblingNameExists(ctx, name, parentID, excludeID)
	if err != nil {
		return fmt.Errorf("check sibling name: %w", err)
	}
	if exists {
		return apperrors.Conflict("BASE_DEPT_NAME_DUPLICATE", "同级下已存在同名科室")
	}
	return nil
}

// normalizeParentID 归一化 ParentID：
// - nil 或 *0 → nil（根科室）
// - *N (N>0) → *N
func normalizeParentID(p *int64) *int64 {
	if p == nil || *p <= 0 {
		return nil
	}
	return p
}

// ============ DTO 转换 ============

func toDepartmentDTO(d *entity.Department) DepartmentDTO {
	return DepartmentDTO{
		ID:        d.ID,
		Name:      d.Name,
		ParentID:  d.ParentID,
		IsPublic:  d.IsPublic,
		IsActive:  d.IsActive,
		CreatedAt: d.CreatedAt,
	}
}

func toTreeDTO(d *entity.Department) DepartmentTreeDTO {
	return DepartmentTreeDTO{
		ID:          d.ID,
		Name:        d.Name,
		ParentID:    d.ParentID,
		IsPublic:    d.IsPublic,
		IsActive:    d.IsActive,
		Description: d.Description,
		CreatedAt:   d.CreatedAt,
		UpdatedAt:   d.UpdatedAt,
	}
}
