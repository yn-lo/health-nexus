package service

import (
	"context"
	"testing"

	"health-nexus/internal/domain/wiki/entity"
	"health-nexus/internal/domain/wiki/repository"
	"health-nexus/internal/shared/constants"
)

// ============================================================================
// Mock 实现
// ============================================================================

type fakeRefTxManager struct{}

func (f *fakeRefTxManager) WithTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

type mockRefRepo struct {
	createErr   error
	getByIDRef  *entity.ArticleReference
	getByIDErr  error
	hasPending  bool
	hasPendErr  error
	hasApproved bool
	hasApprErr  error
}

func (m *mockRefRepo) Create(_ context.Context, ref *entity.ArticleReference) error {
	if m.createErr != nil {
		return m.createErr
	}
	ref.ID = 1
	return nil
}
func (m *mockRefRepo) GetByID(_ context.Context, _ int64) (*entity.ArticleReference, error) {
	if m.getByIDErr != nil {
		return nil, m.getByIDErr
	}
	if m.getByIDRef != nil {
		return m.getByIDRef, nil
	}
	return &entity.ArticleReference{ID: 1, Status: constants.ReferenceStatusApproved}, nil
}
func (m *mockRefRepo) HasPending(_ context.Context, _, _ int64) (bool, error) {
	return m.hasPending, m.hasPendErr
}
func (m *mockRefRepo) HasApproved(_ context.Context, _, _ int64) (bool, error) {
	return m.hasApproved, m.hasApprErr
}
func (m *mockRefRepo) List(_ context.Context, _ repository.ListFilter, _, _ int) ([]*entity.ArticleReference, int64, error) {
	return nil, 0, nil
}
func (m *mockRefRepo) UpdateStatus(_ context.Context, _ int64, _, _ string, _ repository.RefStatusOpts) error {
	return nil
}
func (m *mockRefRepo) RevokeByArticle(_ context.Context, _ int64) (int64, error) {
	return 0, nil
}

type mockArticleLookup struct {
	article *entity.Article
	err     error
}

func (m *mockArticleLookup) GetByID(_ context.Context, _ int64) (*entity.Article, error) {
	return m.article, m.err
}

type mockDeptLookup struct {
	dept *DepartmentInfo
	err  error
}

func (m *mockDeptLookup) GetByID(_ context.Context, _ int64) (*DepartmentInfo, error) {
	return m.dept, m.err
}

type mockRoleResolver struct {
	role string
	err  error
}

func (m *mockRoleResolver) GetRoleByUserID(_ context.Context, _ int64) (string, error) {
	return m.role, m.err
}

// ============================================================================
// 测试：Apply 公共科室文章可发起引用申请（REQ-WIKI-020 方案 B 策展制）
// ============================================================================

func newTestReferenceService(
	ref *mockRefRepo, art *mockArticleLookup, dept *mockDeptLookup,
) *ReferenceService {
	return NewReferenceService(
		ref, art, dept,
		&mockAuditRepo{}, &mockRoleResolver{role: constants.RoleDeptAdmin},
		&fakeRefTxManager{},
	)
}

func TestApply_PublicDeptArticle_ShouldSucceed(t *testing.T) {
	deptID := int64(10)
	targetDeptID := int64(20)

	art := &mockArticleLookup{
		article: &entity.Article{
			ID:             100,
			Title:          "公共健康宣教文章",
			Status:         constants.ArticleStatusPublished,
			AllowReference: true,
			DepartmentID:   &deptID,
		},
	}
	dept := &mockDeptLookup{
		dept: &DepartmentInfo{ID: deptID, Name: "健康宣教中心", IsPublic: true},
	}
	refRepo := &mockRefRepo{
		getByIDRef: &entity.ArticleReference{
			ID:           1,
			ArticleID:    100,
			SourceDeptID: deptID,
			TargetDeptID: targetDeptID,
			Status:       constants.ReferenceStatusPending,
			ApplicantID:  5,
		},
	}

	svc := newTestReferenceService(refRepo, art, dept)

	dto, err := svc.Apply(context.Background(), ApplyInput{
		ArticleID:    100,
		TargetDeptID: targetDeptID,
		Actor:        Actor{UserID: 5, Role: constants.RoleDeptAdmin, DeptID: targetDeptID},
	})

	if err != nil {
		t.Fatalf("Apply public dept article: expected success, got error: %v", err)
	}
	if dto == nil {
		t.Fatal("Apply public dept article: expected non-nil DTO")
	}
	if dto.SourceDeptID != deptID {
		t.Errorf("expected SourceDeptID=%d, got %d", deptID, dto.SourceDeptID)
	}
}

func TestApply_NonPublicDeptArticle_ShouldSucceed(t *testing.T) {
	deptID := int64(10)
	targetDeptID := int64(20)

	art := &mockArticleLookup{
		article: &entity.Article{
			ID:             101,
			Title:          "心内科专业文章",
			Status:         constants.ArticleStatusPublished,
			AllowReference: true,
			DepartmentID:   &deptID,
		},
	}
	dept := &mockDeptLookup{
		dept: &DepartmentInfo{ID: deptID, Name: "心内科", IsPublic: false},
	}
	refRepo := &mockRefRepo{
		getByIDRef: &entity.ArticleReference{
			ID:           1,
			ArticleID:    101,
			SourceDeptID: deptID,
			TargetDeptID: targetDeptID,
			Status:       constants.ReferenceStatusPending,
			ApplicantID:  5,
		},
	}

	svc := newTestReferenceService(refRepo, art, dept)

	dto, err := svc.Apply(context.Background(), ApplyInput{
		ArticleID:    101,
		TargetDeptID: targetDeptID,
		Actor:        Actor{UserID: 5, Role: constants.RoleDeptAdmin, DeptID: targetDeptID},
	})

	if err != nil {
		t.Fatalf("Apply non-public dept article: expected success, got error: %v", err)
	}
	if dto == nil {
		t.Fatal("Apply non-public dept article: expected non-nil DTO")
	}
}

func TestApply_SameDept_ShouldReject(t *testing.T) {
	deptID := int64(10)

	art := &mockArticleLookup{
		article: &entity.Article{
			ID:             102,
			Status:         constants.ArticleStatusPublished,
			AllowReference: true,
			DepartmentID:   &deptID,
		},
	}
	dept := &mockDeptLookup{
		dept: &DepartmentInfo{ID: deptID, Name: "心内科", IsPublic: false},
	}
	refRepo := &mockRefRepo{}

	svc := newTestReferenceService(refRepo, art, dept)

	_, err := svc.Apply(context.Background(), ApplyInput{
		ArticleID:    102,
		TargetDeptID: deptID,
		Actor:        Actor{UserID: 5, Role: constants.RoleDeptAdmin, DeptID: deptID},
	})

	if err == nil {
		t.Fatal("Apply same dept: expected error, got nil")
	}
}

func TestApply_NotAllowed_ShouldReject(t *testing.T) {
	deptID := int64(10)
	targetDeptID := int64(20)

	art := &mockArticleLookup{
		article: &entity.Article{
			ID:             103,
			Status:         constants.ArticleStatusPublished,
			AllowReference: false,
			DepartmentID:   &deptID,
		},
	}
	dept := &mockDeptLookup{
		dept: &DepartmentInfo{ID: deptID, Name: "心内科", IsPublic: false},
	}
	refRepo := &mockRefRepo{}

	svc := newTestReferenceService(refRepo, art, dept)

	_, err := svc.Apply(context.Background(), ApplyInput{
		ArticleID:    103,
		TargetDeptID: targetDeptID,
		Actor:        Actor{UserID: 5, Role: constants.RoleDeptAdmin, DeptID: targetDeptID},
	})

	if err == nil {
		t.Fatal("Apply not-allowed article: expected error, got nil")
	}
}
