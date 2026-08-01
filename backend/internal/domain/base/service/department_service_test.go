// department_service_test.go — DepartmentService 树形 CRUD 核心单元测试。
// 覆盖 Create / ListTree / Get / Update / Delete 关键路径，重点验证：
//   - DEPT_ADMIN 子树范围收口（REQ-BASE-011）
//   - 移动父级时的环路检测（REQ-BASE-009）
//   - 删除前置校验（has children / has users，REQ-BASE-010）
package service

import (
	"context"
	"errors"
	"testing"

	"health-nexus/internal/domain/base/entity"
	"health-nexus/internal/shared/constants"
	apperrors "health-nexus/internal/shared/errors"
)

// ============================================================================
// Mock 手写（interface 隐式实现，conventions.md §5）
// ============================================================================

type mockDeptRepo struct {
	// Create
	createInput *entity.Department
	createErr   error
	createdID   int64

	// GetByID
	getByIDDept *entity.Department
	getByIDErr  error
	// 按 ID 分别返回不同结果（key = dept id）
	getByIDMap map[int64]*entity.Department

	// ListAll
	listAllDepts []*entity.Department
	listAllErr   error

	// ListSubtree: 返回包含 rootID 自身及所有后代
	listSubtreeMap map[int64][]*entity.Department // key = rootID
	listSubtreeErr error

	// ListDescendantIDs: 用于环路检测与范围收口
	descendantIDsMap map[int64][]int64 // key = rootID（不含 root 自身）
	descendantErr    error

	// HasChildren
	hasChildrenMap map[int64]bool
	hasChildrenErr error

	// HasUsers
	hasUsersMap map[int64]bool
	hasUsersErr error

	// UpdateFields
	updateInput   map[string]any
	updateResult  *entity.Department
	updateErr     error
	updateInvoked bool

	// Delete
	deleteErr     error
	deleteInvoked bool

	// ListPublic
	listPublicDepts []*entity.Department
	listPublicErr   error
}

func (m *mockDeptRepo) Create(_ context.Context, d *entity.Department) error {
	m.createInput = d
	if m.createErr != nil {
		return m.createErr
	}
	d.ID = m.createdID
	return nil
}

func (m *mockDeptRepo) GetByID(_ context.Context, id int64) (*entity.Department, error) {
	if m.getByIDErr != nil {
		return nil, m.getByIDErr
	}
	if m.getByIDMap != nil {
		return m.getByIDMap[id], nil
	}
	return m.getByIDDept, nil
}

func (m *mockDeptRepo) ListAll(_ context.Context) ([]*entity.Department, error) {
	return m.listAllDepts, m.listAllErr
}

func (m *mockDeptRepo) ListSubtree(_ context.Context, rootID int64) ([]*entity.Department, error) {
	if m.listSubtreeErr != nil {
		return nil, m.listSubtreeErr
	}
	if m.listSubtreeMap != nil {
		return m.listSubtreeMap[rootID], nil
	}
	return nil, nil
}

func (m *mockDeptRepo) ListDescendantIDs(_ context.Context, rootID int64) ([]int64, error) {
	if m.descendantErr != nil {
		return nil, m.descendantErr
	}
	if m.descendantIDsMap != nil {
		return m.descendantIDsMap[rootID], nil
	}
	return nil, nil
}

func (m *mockDeptRepo) HasChildren(_ context.Context, id int64) (bool, error) {
	if m.hasChildrenErr != nil {
		return false, m.hasChildrenErr
	}
	return m.hasChildrenMap[id], nil
}

func (m *mockDeptRepo) HasUsers(_ context.Context, id int64) (bool, error) {
	if m.hasUsersErr != nil {
		return false, m.hasUsersErr
	}
	return m.hasUsersMap[id], nil
}

func (m *mockDeptRepo) SiblingNameExists(_ context.Context, _ string, _ *int64, _ int64) (bool, error) {
	return false, nil
}

func (m *mockDeptRepo) UpdateFields(_ context.Context, id int64, fields map[string]any) (*entity.Department, error) {
	m.updateInvoked = true
	m.updateInput = fields
	if m.updateErr != nil {
		return nil, m.updateErr
	}
	return m.updateResult, nil
}

func (m *mockDeptRepo) Delete(_ context.Context, id int64) error {
	m.deleteInvoked = true
	return m.deleteErr
}

// 跳过未实现的 ListVisible/GetPrimaryForUser（旧接口）— 它们不在本测试范围。
// 测试中只构造 DepartmentService 的 CRUD 路径，ListVisible 用旧测试覆盖或忽略。
func (m *mockDeptRepo) ListVisible(_ context.Context, _, _ int64, _ bool) ([]*entity.Department, error) {
	return nil, nil
}
func (m *mockDeptRepo) GetPrimaryForUser(_ context.Context, _ int64) (*entity.Department, error) {
	return nil, nil
}

func (m *mockDeptRepo) ListPublic(_ context.Context) ([]*entity.Department, error) {
	return m.listPublicDepts, m.listPublicErr
}

// ============================================================================
// 测试辅助
// ============================================================================

func newSvc(repo *mockDeptRepo) *DepartmentService {
	return &DepartmentService{repo: repo}
}

func ptr[T any](v T) *T { return &v }

func assertAppErrCode(t *testing.T, err error, wantHTTP int, wantCode string) {
	t.Helper()
	if err == nil {
		t.Fatalf("期望 AppError，实际 nil")
	}
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("期望 *AppError，实际 %T: %v", err, err)
	}
	if appErr.HTTP != wantHTTP {
		t.Errorf("期望 HTTP=%d，实际 %d", wantHTTP, appErr.HTTP)
	}
	if appErr.Code != wantCode {
		t.Errorf("期望 Code=%s，实际 %s", wantCode, appErr.Code)
	}
}

// ============================================================================
// Create 测试
// ============================================================================

func TestCreate_HappyPath_SuperAdmin(t *testing.T) {
	repo := &mockDeptRepo{
		createdID: 100,
		getByIDMap: map[int64]*entity.Department{
			1: {ID: 1, Name: "内科", IsActive: true},
		},
	}
	svc := newSvc(repo)
	dto, err := svc.Create(context.Background(), CreateDeptInput{
		Name:        "心内科",
		ParentID:    ptr[int64](1),
		Description: "心血管",
		Actor:       Actor{UserID: 1, Role: constants.RoleSuperAdmin},
	})
	if err != nil {
		t.Fatalf("期望 nil，实际 %v", err)
	}
	if dto.ID != 100 {
		t.Errorf("期望 ID=100，实际 %d", dto.ID)
	}
	if repo.createInput == nil {
		t.Fatal("期望 repo.Create 被调用")
	}
	if repo.createInput.ParentID == nil || *repo.createInput.ParentID != 1 {
		t.Errorf("期望 ParentID=1，实际 %v", repo.createInput.ParentID)
	}
}

func TestCreate_EmptyName_Returns422(t *testing.T) {
	svc := newSvc(&mockDeptRepo{})
	_, err := svc.Create(context.Background(), CreateDeptInput{
		Name:  "",
		Actor: Actor{UserID: 1, Role: constants.RoleSuperAdmin},
	})
	assertAppErrCode(t, err, 422, "BASE_DEPT_NAME_REQUIRED")
}

func TestCreate_NameTooLong_Returns422(t *testing.T) {
	long := make([]byte, 101)
	for i := range long {
		long[i] = 'a'
	}
	svc := newSvc(&mockDeptRepo{})
	_, err := svc.Create(context.Background(), CreateDeptInput{
		Name:  string(long),
		Actor: Actor{UserID: 1, Role: constants.RoleSuperAdmin},
	})
	assertAppErrCode(t, err, 422, "BASE_DEPT_NAME_TOO_LONG")
}

func TestCreate_ParentNotFound_Returns400(t *testing.T) {
	repo := &mockDeptRepo{
		getByIDMap: map[int64]*entity.Department{
			999: nil, // 父科室不存在
		},
	}
	svc := newSvc(repo)
	_, err := svc.Create(context.Background(), CreateDeptInput{
		Name:     "心内科",
		ParentID: ptr[int64](999),
		Actor:    Actor{UserID: 1, Role: constants.RoleSuperAdmin},
	})
	assertAppErrCode(t, err, 400, "BASE_DEPT_PARENT_NOT_FOUND")
}

func TestCreate_DeptAdmin_OutOfScope_Returns403(t *testing.T) {
	// DEPT_ADMIN 主科室为 1，试图在科室 2（非子树）下创建子科室
	repo := &mockDeptRepo{
		descendantIDsMap: map[int64][]int64{
			1: {10, 11}, // 科室 1 的后代
		},
		getByIDMap: map[int64]*entity.Department{
			2: {ID: 2, Name: "外科", IsActive: true},
		},
	}
	svc := newSvc(repo)
	_, err := svc.Create(context.Background(), CreateDeptInput{
		Name:     "骨科",
		ParentID: ptr[int64](2),
		Actor:    Actor{UserID: 9, Role: constants.RoleDeptAdmin, DeptID: 1},
	})
	assertAppErrCode(t, err, 403, "BASE_DEPT_OUT_OF_SCOPE")
}

func TestCreate_DeptAdmin_WithinSubtree_HappyPath(t *testing.T) {
	// DEPT_ADMIN 主科室 1，在后代 10 下创建子科室
	repo := &mockDeptRepo{
		descendantIDsMap: map[int64][]int64{
			1: {10, 11},
		},
		getByIDMap: map[int64]*entity.Department{
			10: {ID: 10, Name: "心内", IsActive: true},
		},
		createdID: 100,
	}
	svc := newSvc(repo)
	_, err := svc.Create(context.Background(), CreateDeptInput{
		Name:     "冠心病病房",
		ParentID: ptr[int64](10),
		Actor:    Actor{UserID: 9, Role: constants.RoleDeptAdmin, DeptID: 1},
	})
	if err != nil {
		t.Fatalf("期望 nil，实际 %v", err)
	}
}

// ============================================================================
// ListTree 测试
// ============================================================================

func TestListTree_SuperAdmin_ReturnsAll(t *testing.T) {
	repo := &mockDeptRepo{
		listAllDepts: []*entity.Department{
			{ID: 1, Name: "内科"},
			{ID: 2, Name: "外科"},
		},
	}
	svc := newSvc(repo)
	dtos, err := svc.ListTree(context.Background(), Actor{UserID: 1, Role: constants.RoleSuperAdmin})
	if err != nil {
		t.Fatalf("期望 nil，实际 %v", err)
	}
	if len(dtos) != 2 {
		t.Errorf("期望 2 项，实际 %d", len(dtos))
	}
}

func TestListTree_DeptAdmin_ReturnsSubtree(t *testing.T) {
	repo := &mockDeptRepo{
		listSubtreeMap: map[int64][]*entity.Department{
			1: {
				{ID: 1, Name: "内科"},
				{ID: 10, Name: "心内科", ParentID: ptr[int64](1)},
			},
		},
	}
	svc := newSvc(repo)
	dtos, err := svc.ListTree(context.Background(), Actor{UserID: 9, Role: constants.RoleDeptAdmin, DeptID: 1})
	if err != nil {
		t.Fatalf("期望 nil，实际 %v", err)
	}
	if len(dtos) != 2 {
		t.Errorf("期望 2 项（含自身 + 后代），实际 %d", len(dtos))
	}
}

// ============================================================================
// Update + 环路检测 测试
// ============================================================================

func TestUpdate_MoveToOwnDescendant_Returns409Cycle(t *testing.T) {
	// 树形：1 → 10 → 100，移动 1 到 100 下应触发 BASE_DEPT_CYCLE
	repo := &mockDeptRepo{
		getByIDMap: map[int64]*entity.Department{
			1:   {ID: 1, Name: "内科", IsActive: true},
			100: {ID: 100, Name: "冠心病病房", ParentID: ptr[int64](10), IsActive: true},
		},
		descendantIDsMap: map[int64][]int64{
			1: {10, 100}, // 1 的后代包含 100
		},
	}
	svc := newSvc(repo)
	_, err := svc.Update(context.Background(), 1, UpdateDeptInput{
		ParentID: ptr[int64](100),
		Actor:    Actor{UserID: 1, Role: constants.RoleSuperAdmin},
	})
	assertAppErrCode(t, err, 409, "BASE_DEPT_CYCLE")
}

func TestUpdate_MoveToSelf_Returns409Cycle(t *testing.T) {
	repo := &mockDeptRepo{
		getByIDMap: map[int64]*entity.Department{
			1: {ID: 1, Name: "内科", IsActive: true},
		},
	}
	svc := newSvc(repo)
	_, err := svc.Update(context.Background(), 1, UpdateDeptInput{
		ParentID: ptr[int64](1),
		Actor:    Actor{UserID: 1, Role: constants.RoleSuperAdmin},
	})
	assertAppErrCode(t, err, 409, "BASE_DEPT_CYCLE")
}

func TestUpdate_MoveToRoot_HappyPath(t *testing.T) {
	repo := &mockDeptRepo{
		getByIDMap: map[int64]*entity.Department{
			1: {ID: 1, Name: "心内科", ParentID: ptr[int64](10), IsActive: true},
		},
		updateResult: &entity.Department{ID: 1, Name: "心内科", ParentID: nil, IsActive: true},
	}
	svc := newSvc(repo)
	dto, err := svc.Update(context.Background(), 1, UpdateDeptInput{
		ParentID: ptr[int64](0), // 0 = 变为根科室（nil parent）
		Actor:    Actor{UserID: 1, Role: constants.RoleSuperAdmin},
	})
	if err != nil {
		t.Fatalf("期望 nil，实际 %v", err)
	}
	if dto.ParentID != nil {
		t.Errorf("期望 ParentID=nil，实际 %v", dto.ParentID)
	}
}

func TestUpdate_AllFieldsNil_Returns422(t *testing.T) {
	svc := newSvc(&mockDeptRepo{})
	_, err := svc.Update(context.Background(), 1, UpdateDeptInput{
		Actor: Actor{UserID: 1, Role: constants.RoleSuperAdmin},
	})
	assertAppErrCode(t, err, 422, "BASE_DEPT_EMPTY_UPDATE")
}

func TestUpdate_NotFound_Returns404(t *testing.T) {
	repo := &mockDeptRepo{
		getByIDMap: map[int64]*entity.Department{
			999: nil,
		},
	}
	svc := newSvc(repo)
	_, err := svc.Update(context.Background(), 999, UpdateDeptInput{
		Name:  ptr("新名"),
		Actor: Actor{UserID: 1, Role: constants.RoleSuperAdmin},
	})
	assertAppErrCode(t, err, 404, "BASE_DEPT_NOT_FOUND")
}

func TestUpdate_DeptAdmin_OutOfScope_Returns403(t *testing.T) {
	// DEPT_ADMIN 主科室 1，试图更新科室 2（非子树）
	repo := &mockDeptRepo{
		descendantIDsMap: map[int64][]int64{
			1: {10, 11},
		},
	}
	svc := newSvc(repo)
	_, err := svc.Update(context.Background(), 2, UpdateDeptInput{
		Name:  ptr("新名"),
		Actor: Actor{UserID: 9, Role: constants.RoleDeptAdmin, DeptID: 1},
	})
	assertAppErrCode(t, err, 403, "BASE_DEPT_OUT_OF_SCOPE")
}

// ============================================================================
// Delete 测试
// ============================================================================

func TestDelete_HasChildren_Returns409(t *testing.T) {
	repo := &mockDeptRepo{
		getByIDMap: map[int64]*entity.Department{
			1: {ID: 1, Name: "内科", IsActive: true},
		},
		hasChildrenMap: map[int64]bool{1: true},
	}
	svc := newSvc(repo)
	_, err := svc.Delete(context.Background(), 1, Actor{UserID: 1, Role: constants.RoleSuperAdmin})
	assertAppErrCode(t, err, 409, "BASE_DEPT_HAS_CHILDREN")
	if repo.deleteInvoked {
		t.Error("不应调用 repo.Delete")
	}
}

func TestDelete_HasUsers_Returns409(t *testing.T) {
	repo := &mockDeptRepo{
		getByIDMap: map[int64]*entity.Department{
			1: {ID: 1, Name: "内科", IsActive: true},
		},
		hasChildrenMap: map[int64]bool{1: false},
		hasUsersMap:    map[int64]bool{1: true},
	}
	svc := newSvc(repo)
	_, err := svc.Delete(context.Background(), 1, Actor{UserID: 1, Role: constants.RoleSuperAdmin})
	assertAppErrCode(t, err, 409, "BASE_DEPT_HAS_USERS")
	if repo.deleteInvoked {
		t.Error("不应调用 repo.Delete")
	}
}

func TestDelete_HappyPath(t *testing.T) {
	repo := &mockDeptRepo{
		getByIDMap: map[int64]*entity.Department{
			1: {ID: 1, Name: "内科", IsActive: true},
		},
		hasChildrenMap: map[int64]bool{1: false},
		hasUsersMap:    map[int64]bool{1: false},
	}
	svc := newSvc(repo)
	res, err := svc.Delete(context.Background(), 1, Actor{UserID: 1, Role: constants.RoleSuperAdmin})
	if err != nil {
		t.Fatalf("期望 nil，实际 %v", err)
	}
	if !res {
		t.Error("期望返回 true")
	}
	if !repo.deleteInvoked {
		t.Error("期望 repo.Delete 被调用")
	}
}

func TestDelete_NotFound_Returns404(t *testing.T) {
	repo := &mockDeptRepo{
		getByIDMap: map[int64]*entity.Department{999: nil},
	}
	svc := newSvc(repo)
	_, err := svc.Delete(context.Background(), 999, Actor{UserID: 1, Role: constants.RoleSuperAdmin})
	assertAppErrCode(t, err, 404, "BASE_DEPT_NOT_FOUND")
}

// ============================================================================
// ListPublic 测试（REQ-BASE-013）
// ============================================================================

func TestListPublic_ReturnsPublicActiveDepartments(t *testing.T) {
	repo := &mockDeptRepo{
		listPublicDepts: []*entity.Department{
			{ID: 1, Name: "内科", IsPublic: true, IsActive: true},
			{ID: 2, Name: "外科", IsPublic: true, IsActive: true},
		},
	}
	svc := newSvc(repo)
	dtos, err := svc.ListPublic(context.Background())
	if err != nil {
		t.Fatalf("期望 nil，实际 %v", err)
	}
	if len(dtos) != 2 {
		t.Fatalf("期望 2 个科室，实际 %d", len(dtos))
	}
	if dtos[0].ID != 1 || dtos[0].Name != "内科" {
		t.Errorf("期望第一个为内科(1)，实际 %s(%d)", dtos[0].Name, dtos[0].ID)
	}
	if dtos[1].ID != 2 || dtos[1].Name != "外科" {
		t.Errorf("期望第二个为外科(2)，实际 %s(%d)", dtos[1].Name, dtos[1].ID)
	}
}

func TestListPublic_EmptyList(t *testing.T) {
	repo := &mockDeptRepo{
		listPublicDepts: []*entity.Department{},
	}
	svc := newSvc(repo)
	dtos, err := svc.ListPublic(context.Background())
	if err != nil {
		t.Fatalf("期望 nil，实际 %v", err)
	}
	if len(dtos) != 0 {
		t.Errorf("期望空列表，实际 %d 个", len(dtos))
	}
}

func TestListPublic_RepoError_Propagates(t *testing.T) {
	repo := &mockDeptRepo{
		listPublicErr: errors.New("db connection lost"),
	}
	svc := newSvc(repo)
	_, err := svc.ListPublic(context.Background())
	if err == nil {
		t.Fatal("期望错误，实际 nil")
	}
}

// ============================================================================
// Get 测试
// ============================================================================

func TestGet_HappyPath(t *testing.T) {
	repo := &mockDeptRepo{
		getByIDMap: map[int64]*entity.Department{
			1: {ID: 1, Name: "内科", IsActive: true, Description: "心血管"},
		},
	}
	svc := newSvc(repo)
	dto, err := svc.Get(context.Background(), 1, Actor{UserID: 1, Role: constants.RoleSuperAdmin})
	if err != nil {
		t.Fatalf("期望 nil，实际 %v", err)
	}
	if dto.ID != 1 || dto.Description != "心血管" {
		t.Errorf("DTO 不匹配: %+v", dto)
	}
}

func TestGet_DeptAdmin_OutOfScope_Returns403(t *testing.T) {
	repo := &mockDeptRepo{
		descendantIDsMap: map[int64][]int64{
			1: {10, 11}, // DEPT_ADMIN 主科室 1 的后代
		},
	}
	svc := newSvc(repo)
	_, err := svc.Get(context.Background(), 2, Actor{UserID: 9, Role: constants.RoleDeptAdmin, DeptID: 1})
	assertAppErrCode(t, err, 403, "BASE_DEPT_OUT_OF_SCOPE")
}
