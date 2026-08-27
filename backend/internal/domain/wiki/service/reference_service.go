package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"health-nexus/internal/domain/wiki/entity"
	"health-nexus/internal/domain/wiki/repository"
	"health-nexus/internal/shared/constants"
	apperrors "health-nexus/internal/shared/errors"
)

// DepartmentInfo 跨域科室信息（仅含 wiki 引用授权需要的字段）。
type DepartmentInfo struct {
	ID       int64
	Name     string
	IsPublic bool
}

// DepartmentLookup 跨域：base 域实现（消费者定义，ISP）。
// 用于引用申请时校验源科室是否为公共科室（REQ-WIKI-020）。
// 阶段 2 由 DI 层注入 base 域适配器。
type DepartmentLookup interface {
	GetByID(ctx context.Context, deptID int64) (*DepartmentInfo, error)
}

// ApplicantRoleResolver 跨域：查询用户角色（消费者定义，ISP）。
// 由 auth 域 adapter 实现，用于 D-MED-08 单超管自审豁免：判断申请人是否为 SUPER_ADMIN。
type ApplicantRoleResolver interface {
	GetRoleByUserID(ctx context.Context, userID int64) (string, error)
}

// ReferenceRepoPort 引用授权持久化能力（消费者定义，ISP）。
type ReferenceRepoPort interface {
	Create(ctx context.Context, ref *entity.ArticleReference) error
	GetByID(ctx context.Context, id int64) (*entity.ArticleReference, error)
	HasPending(ctx context.Context, articleID, targetDeptID int64) (bool, error)
	HasApproved(ctx context.Context, articleID, targetDeptID int64) (bool, error)
	List(ctx context.Context, f repository.ListFilter, limit, offset int) ([]*entity.ArticleReference, int64, error)
	UpdateStatus(ctx context.Context, id int64, fromStatus, toStatus string, opts repository.RefStatusOpts) error
	RevokeByArticle(ctx context.Context, articleID int64) (int64, error)
}

// ArticleLookupPort 引用申请时读取文章（仅 status/allow_reference/department_id）。
// Apply 流程使用：status（已发布校验）、allow_reference（开放引用校验）、department_id（源科室提取）。
// 不使用 author_id——引用申请的鉴权由 Actor（申请人）而非文章作者决定。
// 独立声明的窄接口，便于未来替换为非 ArticleRepo 的实现；当前 DI 直接传入同一个 *ArticleRepo。
type ArticleLookupPort interface {
	GetByID(ctx context.Context, id int64) (*entity.Article, error)
}

// ReferenceService 跨科室引用授权流程（REQ-WIKI-019~022）。
type ReferenceService struct {
	ref          ReferenceRepoPort
	art          ArticleLookupPort
	dept         DepartmentLookup
	audit        AuditRepoPort
	roleResolver ApplicantRoleResolver
	tx           TxRunner
}

// NewReferenceService 构造引用授权服务。dept 必须由 DI 层提供（阶段 2 注入 base 适配器）。
// audit 用于引用授权操作审计落库（D-HIGH-05，REQ-WIKI-002）。
// roleResolver 用于 D-MED-08 单超管自审豁免；可为 nil——退化回原"超管不得自审"行为。
func NewReferenceService(
	ref ReferenceRepoPort, art ArticleLookupPort, dept DepartmentLookup,
	audit AuditRepoPort, roleResolver ApplicantRoleResolver, tx TxRunner,
) *ReferenceService {
	return &ReferenceService{ref: ref, art: art, dept: dept, audit: audit, roleResolver: roleResolver, tx: tx}
}

// ============ DTO ============

// ReferenceDTO 引用授权响应（契约 §5.2）。
// reviewed_at 取 approved_at/revoked_at 中非空者；review_note 映射自 review_comment。
type ReferenceDTO struct {
	ID                  int64      `json:"id"`
	ArticleID           int64      `json:"article_id"`
	ArticleTitle        string     `json:"article_title"`
	SourceDeptID        int64      `json:"source_dept_id"`
	SourceDeptName      string     `json:"source_dept_name"`
	TargetDeptID        int64      `json:"target_dept_id"`
	TargetDeptName      string     `json:"target_dept_name"`
	Status              string     `json:"status"`
	ApplicantID         int64      `json:"applicant_id"`
	ApplicantName       string     `json:"applicant_name"`
	ReviewerID          *int64     `json:"reviewer_id"`
	ReviewedAt          *time.Time `json:"reviewed_at"`
	ReviewNote          *string    `json:"review_note"`
	SourceArticleStatus string     `json:"source_article_status"`
	CreatedAt           time.Time  `json:"created_at"`
}

// ============ Apply ============

// ApplyInput 发起引用申请输入。
type ApplyInput struct {
	ArticleID    int64
	TargetDeptID int64
	Actor        Actor
}

// Apply 发起跨科室引用（契约 §5.1，REQ-WIKI-021）。
// 公开文章（allow_reference=true）：直接创建 approved 状态引用，免审批。
// 非公开文章（allow_reference=false）：返回 400 错误，不可被引用。
// 校验顺序：文章存在(404) → 已发布(400) → allow_reference(400) → 源科室存在(400) →
// 申请人科室(403) → 源≠目标(400) → 创建。
func (s *ReferenceService) Apply(ctx context.Context, in ApplyInput) (*ReferenceDTO, error) {
	if err := validateApplyInput(in); err != nil {
		return nil, err
	}
	article, err := s.art.GetByID(ctx, in.ArticleID)
	if err != nil {
		return nil, translateArticleErr(err)
	}
	sourceDeptID, err := requireDeptID(article)
	if err != nil {
		return nil, err
	}
	// 文章必须已发布
	if article.Status != constants.ArticleStatusPublished {
		return nil, apperrors.BadRequest("WIKI_REF_ARTICLE_NOT_PUBLISHED", "仅已发布文章可被引用")
	}
	// 非公开文章不可被引用
	if !article.AllowReference {
		return nil, apperrors.BadRequest("WIKI_REF_NOT_ALLOWED", "该文章未开放引用授权")
	}
	if err := assertApplicantDept(in.Actor, in.TargetDeptID); err != nil {
		return nil, err
	}
	if err := assertSourceTargetDistinct(sourceDeptID, in.TargetDeptID); err != nil {
		return nil, err
	}
	if err := s.assertSourceDeptExists(ctx, sourceDeptID); err != nil {
		return nil, err
	}

	// 公开文章（allow_reference=true）：直接创建 approved 引用，免审批
	return s.applyDirect(ctx, in, sourceDeptID)
}

// validateApplyInput 校验申请输入 ID 有效性。
func validateApplyInput(in ApplyInput) error {
	if in.ArticleID <= 0 {
		return apperrors.Validation("WIKI_REF_ARTICLE_REQUIRED", "article_id 无效")
	}
	if in.TargetDeptID <= 0 {
		return apperrors.Validation("WIKI_REF_TARGET_DEPT_REQUIRED", "target_dept_id 无效")
	}
	return nil
}

// applyDirect 公开文章直接引用（免审批，直接创建 approved 状态）。
// 校验：无已 approved 引用（防重复）→ 创建 approved 记录 → 审计。
func (s *ReferenceService) applyDirect(ctx context.Context, in ApplyInput, sourceDeptID int64) (*ReferenceDTO, error) {
	// 防重复：同 article_id + target_dept_id 不得有已 approved 的引用
	exists, err := s.ref.HasApproved(ctx, in.ArticleID, in.TargetDeptID)
	if err != nil {
		return nil, fmt.Errorf("check approved reference: %w", err)
	}
	if exists {
		return nil, apperrors.Conflict("WIKI_REF_ALREADY_REFERENCED", "该文章已被本科室引用")
	}
	ref := &entity.ArticleReference{
		ArticleID:    in.ArticleID,
		SourceDeptID: sourceDeptID,
		TargetDeptID: in.TargetDeptID,
		Status:       constants.ReferenceStatusApproved,
		ApplicantID:  in.Actor.UserID,
	}
	if err := s.createReference(ctx, ref); err != nil {
		return nil, err
	}
	// D-HIGH-05: 引用授权操作审计落库（fire-and-forget，事务外）。
	s.writeAudit(ctx, &entity.ArticleAuditLog{
		ArticleID:  ref.ArticleID,
		OperatorID: in.Actor.UserID,
		Action:     entity.AuditActionReferenceApply,
		FromStatus: "",
		ToStatus:   constants.ReferenceStatusApproved,
		Summary:    fmt.Sprintf("reference direct (public article): ref=%d target_dept=%d", ref.ID, in.TargetDeptID),
	})
	// 事务提交后重查，确保 JOIN 字段填充（与 List 端点行为一致）。
	created, err := s.ref.GetByID(ctx, ref.ID)
	if err != nil {
		return nil, fmt.Errorf("reload reference: %w", err)
	}
	dto := toReferenceDTO(created)
	return &dto, nil
}

// RevokeByArticle 撤销指定文章的所有 approved 引用（文章删除/归档时调用）。
// 实现 ReferenceRevoker 接口，由 ArticleService 通过 DI 注入调用。
func (s *ReferenceService) RevokeByArticle(ctx context.Context, articleID int64) error {
	n, err := s.ref.RevokeByArticle(ctx, articleID)
	if err != nil {
		return fmt.Errorf("revoke references by article: %w", err)
	}
	if n > 0 {
		s.writeAudit(ctx, &entity.ArticleAuditLog{
			ArticleID:  articleID,
			OperatorID: 0, // 系统自动撤销
			Action:     entity.AuditActionReferenceRevoke,
			FromStatus: constants.ReferenceStatusApproved,
			ToStatus:   constants.ReferenceStatusRevoked,
			Summary:    fmt.Sprintf("auto revoke on article change: %d references", n),
		})
	}
	return nil
}

// assertApplicantDept 申请人只能为本科室申请（超管除外）。
func assertApplicantDept(actor Actor, targetDeptID int64) error {
	if actor.Role != constants.RoleSuperAdmin && targetDeptID != actor.DeptID {
		return apperrors.Forbidden("WIKI_REF_APPLICANT_DEPT", "只能为本科室发起引用申请")
	}
	return nil
}

// assertSourceTargetDistinct 源 ≠ 目标（同科室无需引用）。
func assertSourceTargetDistinct(sourceDeptID, targetDeptID int64) error {
	if sourceDeptID == targetDeptID {
		return apperrors.BadRequest("WIKI_REF_SAME_DEPT", "源科室与目标科室相同，无需引用授权")
	}
	return nil
}

// assertSourceDeptExists 校验源科室存在（数据完整性）。
// 方案 B（REQ-WIKI-020 策展制）：所有科室文章统一走引用审批流，不区分公共/非公共。
func (s *ReferenceService) assertSourceDeptExists(ctx context.Context, sourceDeptID int64) error {
	dept, err := s.dept.GetByID(ctx, sourceDeptID)
	if err != nil {
		return fmt.Errorf("lookup source department: %w", err)
	}
	if dept == nil {
		return apperrors.BadRequest("WIKI_REF_SOURCE_DEPT_MISSING", "源科室不存在")
	}
	return nil
}

// createReference 事务内创建引用申请。事务仅包裹 Create：HasPending 是事务外的 UX 预检（快速 409），
// 真正的并发安全由 uq_article_refs_pending 部分唯一索引保障——
// Create 撞唯一索引返回 ErrDuplicatePending，翻译为 409 并由 tx 回滚。
func (s *ReferenceService) createReference(ctx context.Context, ref *entity.ArticleReference) error {
	return s.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := s.ref.Create(ctx, ref); err != nil {
			if errors.Is(err, repository.ErrDuplicatePending) {
				return apperrors.Conflict("WIKI_REF_PENDING_EXISTS", "已存在待审核的引用申请")
			}
			return fmt.Errorf("create reference: %w", err)
		}
		return nil
	})
}

// ============ List ============

// ListInput 引用列表查询（契约 §5.2）。
type ListInput struct {
	Status    string // 空 = 全部
	Direction string // outgoing/incoming/空=两者
	Actor     Actor
}

// List 引用授权列表。数据隔离：非超管仅本科室相关；超管全部（REQ-WIKI-022）。
func (s *ReferenceService) List(ctx context.Context, in ListInput, limit, offset int) ([]ReferenceDTO, int64, error) {
	// Medium 4: 入口校验 status/direction 白名单，避免无效参数穿透到 SQL 层。
	if in.Status != "" && !isValidReferenceStatus(in.Status) {
		return nil, 0, apperrors.Validation("WIKI_REF_INVALID_STATUS_PARAM", "status 参数无效")
	}
	if in.Direction != "" && in.Direction != constants.ReferenceDirectionOutgoing &&
		in.Direction != constants.ReferenceDirectionIncoming {
		return nil, 0, apperrors.Validation("WIKI_REF_INVALID_DIRECTION", "direction 参数无效")
	}
	f := repository.ListFilter{Status: in.Status, Direction: in.Direction}
	if in.Actor.Role != constants.RoleSuperAdmin {
		f.CurrentDept = in.Actor.DeptID
		deptIDs := []int64{in.Actor.DeptID}
		f.DeptIDs = deptIDs
	}
	list, total, err := s.ref.List(ctx, f, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list references: %w", err)
	}
	dtos := make([]ReferenceDTO, 0, len(list))
	for _, ref := range list {
		dtos = append(dtos, toReferenceDTO(ref))
	}
	return dtos, total, nil
}

// ============ Approve / Reject / Revoke ============

// ApproveReference 审核通过引用申请（pending → approved，契约 §5.3）。
// 仅源科室 DEPT_ADMIN / SUPER_ADMIN 可审核。
func (s *ReferenceService) ApproveReference(ctx context.Context, id int64, note string, actor Actor) error {
	ref, err := s.ref.GetByID(ctx, id)
	if err != nil {
		return translateReferenceErr(err)
	}
	if err := s.assertCanReviewReference(ctx, ref, actor); err != nil {
		return err
	}
	if ref.Status != constants.ReferenceStatusPending {
		return apperrors.Conflict("WIKI_REF_INVALID_STATUS", "仅待审核申请可审核通过")
	}
	reviewerID := actor.UserID
	if err := s.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := s.ref.UpdateStatus(ctx, id,
			constants.ReferenceStatusPending, constants.ReferenceStatusApproved,
			repository.RefStatusOpts{
				ReviewerID:    &reviewerID,
				ReviewComment: strPtrOrNil(note),
				SetApprovedAt: true,
			}); err != nil {
			return s.translateRefStatusErr(ctx, id, err)
		}
		return nil
	}); err != nil {
		return err
	}
	// D-HIGH-05: 审计落库（fire-and-forget）。
	s.writeAudit(ctx, &entity.ArticleAuditLog{
		ArticleID:  ref.ArticleID,
		OperatorID: actor.UserID,
		Action:     entity.AuditActionReferenceApprove,
		FromStatus: constants.ReferenceStatusPending,
		ToStatus:   constants.ReferenceStatusApproved,
		Summary:    fmt.Sprintf("reference approve: ref=%d", id),
		Reason:     note,
	})
	return nil
}

// RejectReference 驳回引用申请（pending → rejected，契约 §5.4）。
func (s *ReferenceService) RejectReference(ctx context.Context, id int64, reason string, actor Actor) error {
	if reason == "" {
		return apperrors.Validation("WIKI_REF_REASON_REQUIRED", "reason 不能为空")
	}
	ref, err := s.ref.GetByID(ctx, id)
	if err != nil {
		return translateReferenceErr(err)
	}
	if err := s.assertCanReviewReference(ctx, ref, actor); err != nil {
		return err
	}
	if ref.Status != constants.ReferenceStatusPending {
		return apperrors.Conflict("WIKI_REF_INVALID_STATUS", "仅待审核申请可驳回")
	}
	reviewerID := actor.UserID
	if err := s.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := s.ref.UpdateStatus(ctx, id,
			constants.ReferenceStatusPending, constants.ReferenceStatusRejected,
			repository.RefStatusOpts{
				ReviewerID:    &reviewerID,
				ReviewComment: &reason,
			}); err != nil {
			return s.translateRefStatusErr(ctx, id, err)
		}
		return nil
	}); err != nil {
		return err
	}
	// D-HIGH-05: 审计落库（fire-and-forget）。
	s.writeAudit(ctx, &entity.ArticleAuditLog{
		ArticleID:  ref.ArticleID,
		OperatorID: actor.UserID,
		Action:     entity.AuditActionReferenceReject,
		FromStatus: constants.ReferenceStatusPending,
		ToStatus:   constants.ReferenceStatusRejected,
		Summary:    fmt.Sprintf("reference reject: ref=%d", id),
		Reason:     reason,
	})
	return nil
}

// RevokeReference 撤销引用授权（approved → revoked，契约 §5.5）。
// 目标科室不再可见该文章。
func (s *ReferenceService) RevokeReference(ctx context.Context, id int64, actor Actor) error {
	ref, err := s.ref.GetByID(ctx, id)
	if err != nil {
		return translateReferenceErr(err)
	}
	if err := s.assertCanReviewReference(ctx, ref, actor); err != nil {
		return err
	}
	if ref.Status != constants.ReferenceStatusApproved {
		return apperrors.Conflict("WIKI_REF_INVALID_STATUS", "仅已通过授权可撤销")
	}
	if err := s.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := s.ref.UpdateStatus(ctx, id,
			constants.ReferenceStatusApproved, constants.ReferenceStatusRevoked,
			repository.RefStatusOpts{SetRevokedAt: true}); err != nil {
			return s.translateRefStatusErr(ctx, id, err)
		}
		return nil
	}); err != nil {
		return err
	}
	// D-HIGH-05: 审计落库（fire-and-forget）。
	s.writeAudit(ctx, &entity.ArticleAuditLog{
		ArticleID:  ref.ArticleID,
		OperatorID: actor.UserID,
		Action:     entity.AuditActionReferenceRevoke,
		FromStatus: constants.ReferenceStatusApproved,
		ToStatus:   constants.ReferenceStatusRevoked,
		Summary:    fmt.Sprintf("reference revoke: ref=%d", id),
	})
	return nil
}

// ============ 鉴权辅助 ============

// assertCanReviewReference 校验：仅源科室 DEPT_ADMIN / SUPER_ADMIN 可审核/撤销（REQ-WIKI-022）。
// 防止自审：申请人不得审核自己发起的申请（REQ-WIKI-022 隐含，与文章审核 author!=reviewer 对齐）。
//
// D-MED-08 单超管兜底：若申请人为 SUPER_ADMIN，允许同一 SUPER_ADMIN 自审。
// ponytail: 该豁免在多超管场景下仍生效（轻微安全松绑）——避免运维仅 1 名 SUPER_ADMIN 时申请卡死。
// 升级路径：若需严格"多超管不得自审"，可在 auth 域增加"活跃超管计数"，
// 仅 count==1 时豁免；或要求超管申请必须由非申请超管审核（需运维流程保障）。
func (s *ReferenceService) assertCanReviewReference(
	ctx context.Context, ref *entity.ArticleReference, actor Actor,
) error {
	if actor.Role == constants.RoleSuperAdmin {
		if ref.ApplicantID == actor.UserID {
			// 自审：仅当申请人也是 SUPER_ADMIN 时豁免（单超管场景兜底）。
			if s.roleResolver != nil {
				applicantRole, err := s.roleResolver.GetRoleByUserID(ctx, ref.ApplicantID)
				if err != nil {
					// 查询失败 fail closed：保持自审校验，避免误放行。
					slog.ErrorContext(ctx, "wiki: resolve applicant role failed",
						"applicant_id", ref.ApplicantID, "err", err)
				} else if applicantRole == constants.RoleSuperAdmin {
					return nil
				}
			}
			return apperrors.Forbidden("WIKI_REF_SELF_REVIEW", "不得审核自己发起的引用申请")
		}
		return nil
	}
	if actor.Role != constants.RoleDeptAdmin {
		return apperrors.Forbidden("WIKI_REF_REVIEW_FORBIDDEN", "仅科室管理员可操作引用授权")
	}
	if ref.SourceDeptID != actor.DeptID {
		return apperrors.Forbidden("WIKI_REF_DEPT_MISMATCH", "仅源科室管理员可操作")
	}
	if ref.ApplicantID == actor.UserID {
		return apperrors.Forbidden("WIKI_REF_SELF_REVIEW", "不得审核自己发起的引用申请")
	}
	return nil
}

// writeAudit 落库审计日志（fire-and-forget）。
// ponytail: 审计写入失败仅记日志，不阻断主业务；audit 为 nil 时跳过（兼容未注入场景），折中。
func (s *ReferenceService) writeAudit(ctx context.Context, log *entity.ArticleAuditLog) {
	if s.audit == nil {
		return
	}
	if err := s.audit.Create(ctx, log); err != nil {
		slog.ErrorContext(ctx, "wiki: reference audit log failed",
			"action", log.Action,
			"article_id", log.ArticleID,
			"operator_id", log.OperatorID,
			"err", err,
		)
	}
}

// requireDeptID 取文章所属科室 ID，无科室返回 400。
func requireDeptID(a *entity.Article) (int64, error) {
	if a.DepartmentID == nil || *a.DepartmentID <= 0 {
		return 0, apperrors.BadRequest("WIKI_ARTICLE_NO_DEPT", "文章未归属任何科室")
	}
	return *a.DepartmentID, nil
}

// isValidReferenceStatus 引用状态白名单校验（pending|approved|rejected|revoked）。
func isValidReferenceStatus(s string) bool {
	switch s {
	case constants.ReferenceStatusPending, constants.ReferenceStatusApproved,
		constants.ReferenceStatusRejected, constants.ReferenceStatusRevoked:
		return true
	}
	return false
}

// ============ DTO 转换 ============

func toReferenceDTO(ref *entity.ArticleReference) ReferenceDTO {
	var reviewedAt *time.Time
	if ref.ApprovedAt != nil {
		reviewedAt = ref.ApprovedAt
	} else if ref.RevokedAt != nil {
		reviewedAt = ref.RevokedAt
	}
	var note *string
	if ref.ReviewComment != "" {
		n := ref.ReviewComment
		note = &n
	}
	return ReferenceDTO{
		ID:                  ref.ID,
		ArticleID:           ref.ArticleID,
		ArticleTitle:        ref.ArticleTitle,
		SourceDeptID:        ref.SourceDeptID,
		SourceDeptName:      ref.SourceDeptName,
		TargetDeptID:        ref.TargetDeptID,
		TargetDeptName:      ref.TargetDeptName,
		Status:              ref.Status,
		ApplicantID:         ref.ApplicantID,
		ApplicantName:       ref.ApplicantName,
		ReviewerID:          ref.ReviewerID,
		ReviewedAt:          reviewedAt,
		ReviewNote:          note,
		SourceArticleStatus: ref.SourceArticleStatus,
		CreatedAt:           ref.CreatedAt,
	}
}

// translateReferenceErr 把 repo 的 ErrNotFound 翻译为 AppError 404；其他原样返回。
func translateReferenceErr(err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return apperrors.NotFound("WIKI_REF_NOT_FOUND", "引用授权记录不存在")
	}
	return err
}

// translateRefStatusErr 区分 UpdateStatus 的"行不存在(404)"与"状态不匹配(409)"。
// UpdateStatus 用 WHERE status=$from 乐观锁，RowsAffected==0 可能是不存在或并发状态漂移；
// 在同事务内二次 GetByID 区分：存在则 409，不存在则 404。
func (s *ReferenceService) translateRefStatusErr(ctx context.Context, id int64, err error) error {
	return translateStatusErr(ctx, id, err, s.ref.GetByID,
		"WIKI_REF_NOT_FOUND", "引用授权记录不存在", "WIKI_REF_INVALID_STATUS", "状态非预期，无法操作")
}
