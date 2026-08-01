// Package repository 实现 base 域的持久化（手写 SQL + pgx）。
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"health-nexus/internal/domain/base/entity"
	"health-nexus/internal/platform/postgres"
)

// DepartmentRepo 科室仓储，基于 pgx 手写 SQL。
type DepartmentRepo struct {
	pool *pgxpool.Pool
}

// NewDepartmentRepo 构造科室仓储。
func NewDepartmentRepo(pool *pgxpool.Pool) *DepartmentRepo {
	return &DepartmentRepo{pool: pool}
}

// departmentColumns 标准 SELECT 列（含 description，与 entity 字段顺序对齐）。
const departmentColumns = `id, name, parent_id, is_public, is_active, description, created_at, updated_at`

// listVisibleQuery 查询当前用户可见的科室。
// 可见性 = 用户所属科室（user_departments）∪ JWT 主科室（兜底）∪ 公共科室。
// ponytail: 可见性规则简化为三者并集，未实现科室树继承检索（REQ-BASE-001 仅存储 parent_id）。
// 升级路径：若需按树形结构过滤，改用 WITH RECURSIVE 查询祖先/后代（ListSubtree 已实现，可在调用方切换）。
// SELECT 未取 description：DepartmentDTO 未暴露该字段，所有消费者（BaseDepartmentResolver/
// BaseDepartmentLookup）均未读取；entity.Department.Description 字段保留以备未来扩展。
const listVisibleQuery = `
SELECT d.id, d.name, d.parent_id, d.is_public, d.is_active, d.created_at, d.updated_at
FROM departments d
WHERE (
    d.id IN (SELECT department_id FROM user_departments WHERE user_id = $1)
    OR d.id = $2
    OR d.is_public = TRUE
)
AND d.is_active = $3
ORDER BY d.name
`

const listPublicQuery = `
SELECT d.id, d.name, d.parent_id, d.is_public, d.is_active, d.created_at, d.updated_at
FROM departments d
WHERE d.is_public = TRUE AND d.is_active = TRUE
ORDER BY d.name
`

// ListPublic 返回所有公共且启用的科室（REQ-BASE-013）。
// 无需用户上下文，供匿名端点使用。
func (r *DepartmentRepo) ListPublic(ctx context.Context) ([]*entity.Department, error) {
	rows, err := r.pool.Query(ctx, listPublicQuery)
	if err != nil {
		return nil, fmt.Errorf("query public departments: %w", err)
	}
	defer rows.Close()

	result := make([]*entity.Department, 0)
	for rows.Next() {
		d := &entity.Department{}
		err := rows.Scan(
			&d.ID, &d.Name, &d.ParentID, &d.IsPublic, &d.IsActive, &d.CreatedAt, &d.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan department: %w", err)
		}
		result = append(result, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate departments: %w", err)
	}
	return result, nil
}

// ListVisible 返回当前用户可见的科室列表（REQ-BASE-004）。
// userID 来自 JWT，用于查 user_departments；deptID 为 JWT 主科室，兜底可见；
// active 过滤 is_active（true=仅启用可见，false=仅禁用，默认 true）。
func (r *DepartmentRepo) ListVisible(
	ctx context.Context, userID, deptID int64, active bool,
) ([]*entity.Department, error) {
	rows, err := r.pool.Query(ctx, listVisibleQuery, userID, deptID, active)
	if err != nil {
		return nil, fmt.Errorf("query departments: %w", err)
	}
	defer rows.Close()

	result := make([]*entity.Department, 0)
	for rows.Next() {
		d := &entity.Department{}
		err := rows.Scan(
			&d.ID, &d.Name, &d.ParentID, &d.IsPublic, &d.IsActive, &d.CreatedAt, &d.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan department: %w", err)
		}
		result = append(result, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate departments: %w", err)
	}
	return result, nil
}

const getPrimaryForUserQuery = `
SELECT d.id, d.name, d.parent_id, d.is_public, d.is_active, d.created_at, d.updated_at
FROM departments d
JOIN user_departments ud ON ud.department_id = d.id
WHERE ud.user_id = $1 AND ud.is_primary = TRUE AND d.is_active = TRUE
LIMIT 1
`

// GetPrimaryForUser 返回患者主科室（user_departments.is_primary = TRUE 且科室启用）。
// ponytail: LIMIT 1 兜底——历史上 schema 未对 (user_id, is_primary=true) 加部分唯一索引，折中；
// 若数据异常出现多个主科室，取其一；migration 00013 已加 partial unique index，
// LIMIT 1 作为防御性编程保留，升级路径：完全信任索引后可去掉。
// 未绑定主科室或主科室已禁用时返回 (nil, nil)。
func (r *DepartmentRepo) GetPrimaryForUser(ctx context.Context, userID int64) (*entity.Department, error) {
	d := &entity.Department{}
	err := r.pool.QueryRow(ctx, getPrimaryForUserQuery, userID).Scan(
		&d.ID, &d.Name, &d.ParentID, &d.IsPublic, &d.IsActive, &d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get primary department for user: %w", err)
	}
	return d, nil
}

const getByIDQuery = `
SELECT d.id, d.name, d.parent_id, d.is_public, d.is_active, d.description, d.created_at, d.updated_at
FROM departments d
WHERE d.id = $1`

// GetByID 按 ID 精确查询单个科室（不区分 is_active，用于跨域精确查询如引用授权校验）。
// 含 description 字段（管理员视图需要）。未找到返回 (nil, nil)。
func (r *DepartmentRepo) GetByID(ctx context.Context, id int64) (*entity.Department, error) {
	d := &entity.Department{}
	err := r.pool.QueryRow(ctx, getByIDQuery, id).Scan(
		&d.ID, &d.Name, &d.ParentID, &d.IsPublic, &d.IsActive, &d.Description, &d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get department by id: %w", err)
	}
	return d, nil
}

const listAllQuery = `
SELECT ` + departmentColumns + `
FROM departments
ORDER BY parent_id NULLS FIRST, departments.name
`

// ListAll 返回全部科室（含禁用），供管理员维护树形结构（REQ-BASE-006，SUPER_ADMIN 路径）。
// 排序：parent_id NULLS FIRST 让根科室排在前面，便于前端按层级组装。
func (r *DepartmentRepo) ListAll(ctx context.Context) ([]*entity.Department, error) {
	rows, err := r.pool.Query(ctx, listAllQuery)
	if err != nil {
		return nil, fmt.Errorf("query all departments: %w", err)
	}
	defer rows.Close()

	result := make([]*entity.Department, 0)
	for rows.Next() {
		d := &entity.Department{}
		err := rows.Scan(
			&d.ID, &d.Name, &d.ParentID, &d.IsPublic, &d.IsActive,
			&d.Description, &d.CreatedAt, &d.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan department: %w", err)
		}
		result = append(result, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate departments: %w", err)
	}
	return result, nil
}

// listSubtreeQuery 用 WITH RECURSIVE 查询 rootID 子树（含 rootID 自身，REQ-BASE-006/011/012）。
// 递归基：rootID 自身；递归步：children.parent_id = parent.id。
// 防护：PostgreSQL 递归 CTE 默认无深度限制；如出现成环数据（不应发生，service 层环检兜底），
// CTE 仍会终止——因为后代集合不会重复包含已访问节点（UNION 去重）。
// 注意：UNION 两侧与最外层 SELECT 均使用显式列名（非 *），避免 CTE 内外 name/parent_id 歧义。
const listSubtreeQuery = `
WITH RECURSIVE subtree AS (
    SELECT id, name, parent_id, is_public, is_active, description, created_at, updated_at
    FROM departments WHERE id = $1
    UNION
    SELECT d.id, d.name, d.parent_id, d.is_public, d.is_active, d.description, d.created_at, d.updated_at
    FROM departments d
    INNER JOIN subtree s ON d.parent_id = s.id
)
SELECT id, name, parent_id, is_public, is_active, description, created_at, updated_at FROM subtree
ORDER BY parent_id NULLS FIRST, name
`

// ListSubtree 返回 rootID 子树（含 rootID 自身）的全部科室（含禁用），供 DEPT_ADMIN 维护（REQ-BASE-006/011）。
// rootID 不存在时返回空切片（CTE 基例为空集）。
func (r *DepartmentRepo) ListSubtree(ctx context.Context, rootID int64) ([]*entity.Department, error) {
	if rootID <= 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, listSubtreeQuery, rootID)
	if err != nil {
		return nil, fmt.Errorf("query subtree: %w", err)
	}
	defer rows.Close()

	result := make([]*entity.Department, 0)
	for rows.Next() {
		d := &entity.Department{}
		err := rows.Scan(
			&d.ID, &d.Name, &d.ParentID, &d.IsPublic, &d.IsActive,
			&d.Description, &d.CreatedAt, &d.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan department: %w", err)
		}
		result = append(result, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subtree: %w", err)
	}
	return result, nil
}

// listDescendantIDsQuery 仅返回 rootID 后代的 ID 列表（不含 rootID 自身），用于环路检测与权限范围校验。
// 不含 root 自身：环路检测时 "new_parent ∈ descendants(self)" 才是非法，self 本身由调用方单独判等。
const listDescendantIDsQuery = `
WITH RECURSIVE descendants AS (
    SELECT id FROM departments WHERE parent_id = $1
    UNION
    SELECT d.id
    FROM departments d
    INNER JOIN descendants ds ON d.parent_id = ds.id
)
SELECT id FROM descendants
`

// ListDescendantIDs 返回 rootID 的全部后代 ID（不含 rootID 自身，REQ-BASE-012）。
// 用于：(1) Update 时环路检测——new_parent_id 不能 ∈ descendants(self)；
//
//	(2) DEPT_ADMIN 范围收口——targetDeptID 必须 ∈ {self} ∪ descendants(self)。
func (r *DepartmentRepo) ListDescendantIDs(ctx context.Context, rootID int64) ([]int64, error) {
	if rootID <= 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, listDescendantIDsQuery, rootID)
	if err != nil {
		return nil, fmt.Errorf("query descendant ids: %w", err)
	}
	defer rows.Close()

	result := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan descendant id: %w", err)
		}
		result = append(result, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate descendant ids: %w", err)
	}
	return result, nil
}

const hasChildrenQuery = `SELECT EXISTS(SELECT 1 FROM departments WHERE parent_id = $1)`

// HasChildren 检查指定科室是否有子科室（REQ-BASE-010 删除前置校验）。
func (r *DepartmentRepo) HasChildren(ctx context.Context, id int64) (bool, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, hasChildrenQuery, id).Scan(&exists); err != nil {
		return false, fmt.Errorf("check children: %w", err)
	}
	return exists, nil
}

const hasUsersQuery = `SELECT EXISTS(SELECT 1 FROM user_departments WHERE department_id = $1)`

// HasUsers 检查指定科室是否有关联用户（REQ-BASE-010 删除前置校验）。
// 注意：schema 中 user_departments.department_id ON DELETE CASCADE，
// 但 service 层选择显式拦截，避免误删导致用户失去主科室关联。
func (r *DepartmentRepo) HasUsers(ctx context.Context, id int64) (bool, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, hasUsersQuery, id).Scan(&exists); err != nil {
		return false, fmt.Errorf("check users: %w", err)
	}
	return exists, nil
}

const siblingNameExistsQuery = `
SELECT EXISTS(
	SELECT 1 FROM departments
	WHERE name = $1
	AND ($2::BIGINT IS NULL AND parent_id IS NULL OR parent_id = $2)
	AND id != $3
)`

func (r *DepartmentRepo) SiblingNameExists(
	ctx context.Context, name string, parentID *int64, excludeID int64,
) (bool, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, siblingNameExistsQuery, name, parentID, excludeID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check sibling name: %w", err)
	}
	return exists, nil
}

const createDeptQuery = `
INSERT INTO departments (name, parent_id, is_public, is_active, description)
VALUES ($1, $2, $3, $4, $5)
RETURNING ` + departmentColumns

// Create 插入新科室（REQ-BASE-005）。parent_id 为 nil 表示根科室。
// 通过 RETURNING 回填 ID/CreatedAt/UpdatedAt 到 d，避免调用方二次查询。
// 调用方需在事务中调用以参与 service 层事务（postgres.TxFromCtx）。
func (r *DepartmentRepo) Create(ctx context.Context, d *entity.Department) error {
	tx, _ := postgres.TxFromCtx(ctx)
	exec := r.pool.QueryRow
	if tx != nil {
		exec = tx.QueryRow
	}
	row := exec(ctx, createDeptQuery, d.Name, d.ParentID, d.IsPublic, d.IsActive, d.Description)
	if err := row.Scan(
		&d.ID, &d.Name, &d.ParentID, &d.IsPublic, &d.IsActive,
		&d.Description, &d.CreatedAt, &d.UpdatedAt,
	); err != nil {
		return fmt.Errorf("insert department: %w", err)
	}
	return nil
}

// updateDeptQuery 动态构造 UPDATE——仅更新非 nil 字段。
// 用 map[string]any 传参，service 层决定哪些字段更新；repo 不感知业务字段语义。
// 调用方需保证 fields 至少有一项（service 层已校验 BASE_DEPT_EMPTY_UPDATE）。
func (r *DepartmentRepo) UpdateFields(
	ctx context.Context, id int64, fields map[string]any,
) (*entity.Department, error) {
	if len(fields) == 0 {
		return nil, fmt.Errorf("UpdateFields called with empty fields map")
	}
	setClauses := make([]string, 0, len(fields))
	args := make([]any, 0, len(fields)+1)
	args = append(args, id)
	i := 2 // $1 = id
	for k, v := range fields {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", k, i))
		args = append(args, v)
		i++
	}
	query := fmt.Sprintf(
		`UPDATE departments SET %s, updated_at = now() WHERE id = $1 RETURNING %s`,
		joinSQL(setClauses, ", "), departmentColumns,
	)
	tx, _ := postgres.TxFromCtx(ctx)
	exec := r.pool.QueryRow
	if tx != nil {
		exec = tx.QueryRow
	}
	d := &entity.Department{}
	if err := exec(ctx, query, args...).Scan(
		&d.ID, &d.Name, &d.ParentID, &d.IsPublic, &d.IsActive, &d.Description, &d.CreatedAt, &d.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("update department: %w", err)
	}
	return d, nil
}

// joinSQL 简单字符串拼接（替代 strings.Join 以避免导入——保持文件依赖最小化）。
// ponytail: 仅在本文件内使用，元素数量受 fields 数量限制（≤ 6），无性能问题，简化。
// 升级路径：如复用，迁移到 internal/shared/sqlutil 包。
func joinSQL(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += sep + p
	}
	return out
}

const deleteDeptQuery = `DELETE FROM departments WHERE id = $1`

// Delete 删除科室（REQ-BASE-010）。schema ON DELETE RESTRICT 兜底——若存在子科室，
// 此调用会失败；service 层已在调用前用 HasChildren 显式校验给出可读错误码。
func (r *DepartmentRepo) Delete(ctx context.Context, id int64) error {
	tx, _ := postgres.TxFromCtx(ctx)
	exec := r.pool.Exec
	if tx != nil {
		exec = tx.Exec
	}
	if _, err := exec(ctx, deleteDeptQuery, id); err != nil {
		return fmt.Errorf("delete department: %w", err)
	}
	return nil
}
