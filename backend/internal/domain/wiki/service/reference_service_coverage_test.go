// reference_service_coverage_test.go — 补齐引用审批/列表/撤销与状态翻译路径的覆盖。
package service

import (
	"context"
	"errors"
	"testing"

	"health-nexus/internal/domain/wiki/entity"
	"health-nexus/internal/domain/wiki/repository"
	"health-nexus/internal/shared/constants"
	apperrors "health-nexus/internal/shared/errors"
)

func assertWikiAppErr(t *testing.T, err error, wantHTTP int, wantCode string) {
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

func refFixture(status string) *entity.ArticleReference {
	return &entity.ArticleReference{
		ID: 1, ArticleID: 100, SourceDeptID: 10, TargetDeptID: 20,
		Status: status, ApplicantID: 5,
	}
}

func TestApproveReference(t *testing.T) {
	ctx := context.Background()
	srcAdmin := Actor{UserID: 1, Role: constants.RoleDeptAdmin, DeptID: 10}

	t.Run("成功（源科室管理员）", func(t *testing.T) {
		svc := newTestReferenceService(&mockRefRepo{getByIDRef: refFixture(constants.ReferenceStatusPending)}, nil, nil)
		if err := svc.ApproveReference(ctx, 1, "ok", srcAdmin); err != nil {
			t.Fatalf("期望 nil，实际 %v", err)
		}
	})
	t.Run("超管可审", func(t *testing.T) {
		svc := newTestReferenceService(&mockRefRepo{getByIDRef: refFixture(constants.ReferenceStatusPending)}, nil, nil)
		if err := svc.ApproveReference(ctx, 1, "", Actor{UserID: 1, Role: constants.RoleSuperAdmin}); err != nil {
			t.Fatalf("期望 nil，实际 %v", err)
		}
	})
	t.Run("状态非 pending → 409", func(t *testing.T) {
		svc := newTestReferenceService(&mockRefRepo{getByIDRef: refFixture(constants.ReferenceStatusApproved)}, nil, nil)
		assertWikiAppErr(t, svc.ApproveReference(ctx, 1, "", srcAdmin), 409, "WIKI_REF_INVALID_STATUS")
	})
	t.Run("非源科室 → 403", func(t *testing.T) {
		svc := newTestReferenceService(&mockRefRepo{getByIDRef: refFixture(constants.ReferenceStatusPending)}, nil, nil)
		assertWikiAppErr(t, svc.ApproveReference(ctx, 1, "", Actor{UserID: 1, Role: constants.RoleDeptAdmin, DeptID: 99}), 403, "WIKI_REF_DEPT_MISMATCH")
	})
	t.Run("自审 → 403", func(t *testing.T) {
		ref := refFixture(constants.ReferenceStatusPending)
		ref.ApplicantID = 1
		svc := newTestReferenceService(&mockRefRepo{getByIDRef: ref}, nil, nil)
		assertWikiAppErr(t, svc.ApproveReference(ctx, 1, "", Actor{UserID: 1, Role: constants.RoleDeptAdmin, DeptID: 10}), 403, "WIKI_REF_SELF_REVIEW")
	})
	t.Run("记录不存在 → 404", func(t *testing.T) {
		svc := newTestReferenceService(&mockRefRepo{getByIDErr: repository.ErrNotFound}, nil, nil)
		assertWikiAppErr(t, svc.ApproveReference(ctx, 1, "", srcAdmin), 404, "WIKI_REF_NOT_FOUND")
	})
}

func TestRejectReference(t *testing.T) {
	ctx := context.Background()
	srcAdmin := Actor{UserID: 1, Role: constants.RoleDeptAdmin, DeptID: 10}

	t.Run("缺 reason → 422", func(t *testing.T) {
		svc := newTestReferenceService(&mockRefRepo{getByIDRef: refFixture(constants.ReferenceStatusPending)}, nil, nil)
		assertWikiAppErr(t, svc.RejectReference(ctx, 1, "", srcAdmin), 422, "WIKI_REF_REASON_REQUIRED")
	})
	t.Run("成功", func(t *testing.T) {
		svc := newTestReferenceService(&mockRefRepo{getByIDRef: refFixture(constants.ReferenceStatusPending)}, nil, nil)
		if err := svc.RejectReference(ctx, 1, "不适用", srcAdmin); err != nil {
			t.Fatalf("期望 nil，实际 %v", err)
		}
	})
	t.Run("状态非 pending → 409", func(t *testing.T) {
		svc := newTestReferenceService(&mockRefRepo{getByIDRef: refFixture(constants.ReferenceStatusRevoked)}, nil, nil)
		assertWikiAppErr(t, svc.RejectReference(ctx, 1, "x", srcAdmin), 409, "WIKI_REF_INVALID_STATUS")
	})
}

func TestRevokeReference(t *testing.T) {
	ctx := context.Background()
	srcAdmin := Actor{UserID: 1, Role: constants.RoleDeptAdmin, DeptID: 10}

	t.Run("成功", func(t *testing.T) {
		svc := newTestReferenceService(&mockRefRepo{getByIDRef: refFixture(constants.ReferenceStatusApproved)}, nil, nil)
		if err := svc.RevokeReference(ctx, 1, srcAdmin); err != nil {
			t.Fatalf("期望 nil，实际 %v", err)
		}
	})
	t.Run("状态非 approved → 409", func(t *testing.T) {
		svc := newTestReferenceService(&mockRefRepo{getByIDRef: refFixture(constants.ReferenceStatusPending)}, nil, nil)
		assertWikiAppErr(t, svc.RevokeReference(ctx, 1, srcAdmin), 409, "WIKI_REF_INVALID_STATUS")
	})
}

func TestReferenceList(t *testing.T) {
	ctx := context.Background()
	repo := &mockRefRepo{listRefs: []*entity.ArticleReference{refFixture(constants.ReferenceStatusApproved)}, listTotal: 1}
	svc := newTestReferenceService(repo, nil, nil)

	t.Run("非超管按科室过滤", func(t *testing.T) {
		dtos, total, err := svc.List(ctx, ListInput{Actor: Actor{Role: constants.RoleDeptAdmin, DeptID: 10}}, 20, 0)
		if err != nil || total != 1 || len(dtos) != 1 {
			t.Fatalf("结果不符: dtos=%d total=%d err=%v", len(dtos), total, err)
		}
	})
	t.Run("超管全部", func(t *testing.T) {
		if _, _, err := svc.List(ctx, ListInput{Actor: Actor{Role: constants.RoleSuperAdmin}}, 20, 0); err != nil {
			t.Fatalf("期望 nil，实际 %v", err)
		}
	})
	t.Run("非法 status → 422", func(t *testing.T) {
		assertWikiAppErr(t, func() error { _, _, e := svc.List(ctx, ListInput{Status: "bogus"}, 20, 0); return e }(), 422, "WIKI_REF_INVALID_STATUS_PARAM")
	})
	t.Run("非法 direction → 422", func(t *testing.T) {
		assertWikiAppErr(t, func() error { _, _, e := svc.List(ctx, ListInput{Direction: "sideways"}, 20, 0); return e }(), 422, "WIKI_REF_INVALID_DIRECTION")
	})
	t.Run("仓储错误冒泡", func(t *testing.T) {
		bad := newTestReferenceService(&mockRefRepo{listErr: errors.New("db down")}, nil, nil)
		if _, _, err := bad.List(ctx, ListInput{Actor: Actor{Role: constants.RoleSuperAdmin}}, 20, 0); err == nil {
			t.Fatal("期望错误，实际 nil")
		}
	})
}

func TestRevokeByArticle(t *testing.T) {
	ctx := context.Background()
	t.Run("成功", func(t *testing.T) {
		svc := newTestReferenceService(&mockRefRepo{revokeCount: 3}, nil, nil)
		if err := svc.RevokeByArticle(ctx, 100); err != nil {
			t.Fatalf("期望 nil，实际 %v", err)
		}
	})
	t.Run("仓储错误冒泡", func(t *testing.T) {
		svc := newTestReferenceService(&mockRefRepo{revokeErr: errors.New("db down")}, nil, nil)
		if err := svc.RevokeByArticle(ctx, 100); err == nil {
			t.Fatal("期望错误，实际 nil")
		}
	})
}

func TestTranslateReferenceErr(t *testing.T) {
	if err := translateReferenceErr(repository.ErrNotFound); err == nil {
		t.Error("期望 404 翻译")
	}
	sentinel := errors.New("boom")
	if err := translateReferenceErr(sentinel); !errors.Is(err, sentinel) {
		t.Error("非 ErrNotFound 应原样返回")
	}
}

func TestIsValidReferenceStatus(t *testing.T) {
	for _, s := range []string{constants.ReferenceStatusPending, constants.ReferenceStatusApproved,
		constants.ReferenceStatusRejected, constants.ReferenceStatusRevoked} {
		if !isValidReferenceStatus(s) {
			t.Errorf("%q 应为合法状态", s)
		}
	}
	if isValidReferenceStatus("unknown") {
		t.Error("unknown 不应合法")
	}
}
