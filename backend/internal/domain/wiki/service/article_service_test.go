// ArticleService 单元测试：聚焦文章生命周期与切片联动的数据卫生。
// 覆盖 Delete/Archive 在事务内失效切片（REQ-WIKI-016 数据卫生补齐）。
// 覆盖 Approve/Update/Unarchive 事务内写 outbox 保证向量化最终一致性。
package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"health-nexus/internal/domain/wiki/entity"
	"health-nexus/internal/domain/wiki/repository"
	"health-nexus/internal/shared/constants"
	apperrors "health-nexus/internal/shared/errors"
)

// ============================================================================
// 测试辅助：mock 实现
// ============================================================================

// fakeTxRunner 直接同步执行 fn，不开真实事务；记录 fn 是否被调用。
type fakeTxRunner struct {
	called bool
}

func (f *fakeTxRunner) WithTx(ctx context.Context, fn func(ctx context.Context) error) error {
	f.called = true
	return fn(ctx)
}

// mockArticleRepo 模拟 ArticleRepoPort。
type mockArticleRepo struct {
	article       *entity.Article
	getErr        error
	softDeleteErr error
	updateStatErr error
	softDeleteID  int64
	featuredRank  int
	featuredID    int64
}

func (m *mockArticleRepo) Create(_ context.Context, _ *entity.Article) error { return nil }
func (m *mockArticleRepo) GetByID(_ context.Context, _ int64) (*entity.Article, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.article, nil
}
func (m *mockArticleRepo) GetPublishedByID(_ context.Context, _ int64) (*entity.Article, error) {
	return m.article, nil
}
func (m *mockArticleRepo) ListPublished(_ context.Context, _ repository.ListPublishedFilter, _, _ int) ([]*entity.Article, int64, error) {
	return nil, 0, nil
}
func (m *mockArticleRepo) ListFeatured(_ context.Context, _ *int64, _ int) ([]*entity.Article, error) {
	return nil, nil
}
func (m *mockArticleRepo) SetFeaturedRank(_ context.Context, id int64, rank int) error {
	m.featuredID = id
	m.featuredRank = rank
	return nil
}
func (m *mockArticleRepo) ListForStaff(_ context.Context, _ repository.ListStaffFilter, _, _ int) ([]*entity.Article, int64, error) {
	return nil, 0, nil
}
func (m *mockArticleRepo) UpdateFields(_ context.Context, _ int64, _ repository.UpdateFields) (*entity.Article, error) {
	return m.article, nil
}
func (m *mockArticleRepo) UpdateStatus(_ context.Context, _ int64, _, _ string, _ repository.StatusUpdateOpts) error {
	return m.updateStatErr
}
func (m *mockArticleRepo) SoftDelete(_ context.Context, id int64) error {
	m.softDeleteID = id
	return m.softDeleteErr
}

// mockAuditRepo 模拟 AuditRepoPort。
type mockAuditRepo struct {
	createErr error
	createCnt int
}

func (m *mockAuditRepo) Create(_ context.Context, _ *entity.ArticleAuditLog) error {
	m.createCnt++
	return m.createErr
}

// mockChunkRepo 记录 DeactivateByArticle 调用，用于断言切片失效联动。
type mockChunkRepo struct {
	deactivateErr  error
	deactivateID   int64
	deactivateCall int
}

func (m *mockChunkRepo) DeactivateByArticle(_ context.Context, articleID int64) (int64, error) {
	m.deactivateCall++
	m.deactivateID = articleID
	if m.deactivateErr != nil {
		return 0, m.deactivateErr
	}
	return 0, nil
}
func (m *mockChunkRepo) ListActiveByArticle(_ context.Context, _ int64) ([]*entity.ArticleChunk, error) {
	return nil, nil
}

// mockOutboxRepo 记录 outbox Insert 调用，用于断言事务内 outbox 写入。
type mockOutboxRepo struct {
	insertErr  error
	insertID   int64
	insertCall int
}

func (m *mockOutboxRepo) Insert(_ context.Context, articleID int64) error {
	m.insertCall++
	m.insertID = articleID
	return m.insertErr
}

// mockVectorEnqueuer 模拟 VectorizeEnqueuer。
type mockVectorEnqueuer struct {
	err        error
	enqueueID  int64
	enqueueCnt int
}

func (m *mockVectorEnqueuer) Enqueue(_ context.Context, articleID int64) error {
	m.enqueueCnt++
	m.enqueueID = articleID
	return m.err
}

// buildSvc 构造一个 ArticleService，注入各 mock，返回 svc 与各 mock 便于断言。
func buildSvc(article *entity.Article) (*ArticleService, *mockArticleRepo, *mockAuditRepo, *mockChunkRepo, *fakeTxRunner) {
	repo := &mockArticleRepo{article: article}
	audit := &mockAuditRepo{}
	chunks := &mockChunkRepo{}
	tx := &fakeTxRunner{}
	svc := NewArticleService(repo, audit, chunks, tx, &mockVectorEnqueuer{}, nil, nil)
	return svc, repo, audit, chunks, tx
}

// svcDeps 聚合 buildSvcWithOutbox 构造出的服务与各 mock，供测试断言。
type svcDeps struct {
	svc    *ArticleService
	repo   *mockArticleRepo
	audit  *mockAuditRepo
	chunks *mockChunkRepo
	outbox *mockOutboxRepo
	vector *mockVectorEnqueuer
	tx     *fakeTxRunner
}

// buildSvcWithOutbox 构造带 outbox 的 ArticleService。
func buildSvcWithOutbox(article *entity.Article) svcDeps {
	repo := &mockArticleRepo{article: article}
	audit := &mockAuditRepo{}
	chunks := &mockChunkRepo{}
	outbox := &mockOutboxRepo{}
	vector := &mockVectorEnqueuer{}
	tx := &fakeTxRunner{}
	svc := NewArticleService(repo, audit, chunks, tx, vector, outbox, nil)
	return svcDeps{svc: svc, repo: repo, audit: audit, chunks: chunks, outbox: outbox, vector: vector, tx: tx}
}

// ============================================================================
// Delete：软删除应同步失效切片（数据卫生）
// ============================================================================

func TestArticleService_Delete_DeactivatesChunks(t *testing.T) {
	deptID := int64(10)
	article := &entity.Article{
		ID:           42,
		Status:       constants.ArticleStatusPublished,
		AuthorID:     1,
		DepartmentID: &deptID,
	}
	svc, repo, audit, chunks, tx := buildSvc(article)
	actor := Actor{UserID: 1, Role: constants.RoleDoctor, DeptID: 10}

	if err := svc.Delete(context.Background(), 42, actor); err != nil {
		t.Fatalf("Delete 返回错误: %v", err)
	}
	if !tx.called {
		t.Error("期望 WithTx 被调用")
	}
	if repo.softDeleteID != 42 {
		t.Errorf("期望 SoftDelete(42)，实际 %d", repo.softDeleteID)
	}
	if audit.createCnt != 1 {
		t.Errorf("期望审计写入 1 条，实际 %d", audit.createCnt)
	}
	// 核心断言：删除文章应失效其切片，避免 DB 残留 active 切片。
	if chunks.deactivateCall != 1 {
		t.Fatalf("期望 DeactivateByArticle 调用 1 次，实际 %d", chunks.deactivateCall)
	}
	if chunks.deactivateID != 42 {
		t.Errorf("期望 DeactivateByArticle(42)，实际 %d", chunks.deactivateID)
	}
}

func TestArticleService_Delete_ChunkDeactivateFails_RollsBack(t *testing.T) {
	article := &entity.Article{
		ID:       42,
		Status:   constants.ArticleStatusPublished,
		AuthorID: 1,
	}
	svc, _, _, chunks, _ := buildSvc(article)
	chunks.deactivateErr = errors.New("deactivate db error")
	actor := Actor{UserID: 1, Role: constants.RoleDoctor}

	err := svc.Delete(context.Background(), 42, actor)
	if err == nil {
		t.Fatal("期望 Delete 返回错误（切片失效失败应中断）")
	}
	if !errors.Is(err, chunks.deactivateErr) {
		t.Errorf("期望错误包装 deactivateErr，实际 %v", err)
	}
}

// ============================================================================
// Archive：归档应同步失效切片（archived 文章不应再被检索命中）
// ============================================================================

func TestArticleService_Archive_DeactivatesChunks(t *testing.T) {
	deptID := int64(10)
	article := &entity.Article{
		ID:           42,
		Status:       constants.ArticleStatusPublished,
		AuthorID:     1,
		DepartmentID: &deptID,
	}
	svc, _, audit, chunks, tx := buildSvc(article)
	actor := Actor{UserID: 1, Role: constants.RoleDoctor, DeptID: 10}

	if err := svc.Archive(context.Background(), 42, actor); err != nil {
		t.Fatalf("Archive 返回错误: %v", err)
	}
	if !tx.called {
		t.Error("期望 WithTx 被调用")
	}
	if audit.createCnt != 1 {
		t.Errorf("期望审计写入 1 条，实际 %d", audit.createCnt)
	}
	// 核心断言：归档文章应失效其切片。
	if chunks.deactivateCall != 1 {
		t.Fatalf("期望 DeactivateByArticle 调用 1 次，实际 %d", chunks.deactivateCall)
	}
	if chunks.deactivateID != 42 {
		t.Errorf("期望 DeactivateByArticle(42)，实际 %d", chunks.deactivateID)
	}
}

// ============================================================================
// Approve：审核通过应事务内写 outbox，保证向量化最终一致性
// ============================================================================

func TestArticleService_Approve_WritesOutboxInTx(t *testing.T) {
	deptID := int64(10)
	article := &entity.Article{
		ID:           42,
		Status:       constants.ArticleStatusPending,
		AuthorID:     1, // 作者
		DepartmentID: &deptID,
	}
	d := buildSvcWithOutbox(article)
	// 审核人不能是作者
	reviewer := Actor{UserID: 2, Role: constants.RoleDeptAdmin, DeptID: 10}

	if err := d.svc.Approve(context.Background(), ApproveInput{
		ArticleID: 42,
		Actor:     reviewer,
	}); err != nil {
		t.Fatalf("Approve 返回错误: %v", err)
	}
	if !d.tx.called {
		t.Error("期望 WithTx 被调用")
	}
	if d.audit.createCnt != 1 {
		t.Errorf("期望审计写入 1 条，实际 %d", d.audit.createCnt)
	}
	// 核心断言：事务内应写 outbox 记录，保证向量化最终投递。
	if d.outbox.insertCall != 1 {
		t.Fatalf("期望 outbox.Insert 调用 1 次，实际 %d", d.outbox.insertCall)
	}
	if d.outbox.insertID != 42 {
		t.Errorf("期望 outbox.Insert(42)，实际 %d", d.outbox.insertID)
	}
	// 事务外仍尝试直接 Enqueue（快速路径）。
	if d.vector.enqueueCnt != 1 {
		t.Errorf("期望 Enqueue 调用 1 次，实际 %d", d.vector.enqueueCnt)
	}
}

func TestArticleService_Approve_EnqueueFails_OutboxGuaranteesDelivery(t *testing.T) {
	deptID := int64(10)
	article := &entity.Article{
		ID:           42,
		Status:       constants.ArticleStatusPending,
		AuthorID:     1,
		DepartmentID: &deptID,
	}
	d := buildSvcWithOutbox(article)
	d.vector.err = errors.New("asynq down") // 模拟入队失败
	reviewer := Actor{UserID: 2, Role: constants.RoleDeptAdmin, DeptID: 10}

	err := d.svc.Approve(context.Background(), ApproveInput{
		ArticleID: 42,
		Actor:     reviewer,
	})
	// 入队失败不应导致 Approve 失败——outbox 兜底。
	if err != nil {
		t.Fatalf("Approve 不应因入队失败而返回错误: %v", err)
	}
	// outbox 记录已写入事务内，relay 会兜底投递。
	if d.outbox.insertCall != 1 {
		t.Fatalf("期望 outbox.Insert 调用 1 次（入队失败时 outbox 兜底），实际 %d", d.outbox.insertCall)
	}
}

// ============================================================================
// Update：已发布文章内容变更应事务内写 outbox
// ============================================================================

func TestArticleService_Update_PublishedContentChange_WritesOutbox(t *testing.T) {
	deptID := int64(10)
	article := &entity.Article{
		ID:           42,
		Status:       constants.ArticleStatusPublished,
		AuthorID:     1,
		DepartmentID: &deptID,
		Content:      "old content",
		ContentHash:  "old_hash",
	}
	d := buildSvcWithOutbox(article)
	actor := Actor{UserID: 1, Role: constants.RoleDoctor, DeptID: 10}

	newContent := "new content"
	_, err := d.svc.Update(context.Background(), UpdateInput{
		Content:   &newContent,
		ArticleID: 42,
		Actor:     actor,
	})
	if err != nil {
		t.Fatalf("Update 返回错误: %v", err)
	}
	// 核心断言：已发布文章内容变更应事务内写 outbox。
	if d.outbox.insertCall != 1 {
		t.Fatalf("期望 outbox.Insert 调用 1 次，实际 %d", d.outbox.insertCall)
	}
	if d.outbox.insertID != 42 {
		t.Errorf("期望 outbox.Insert(42)，实际 %d", d.outbox.insertID)
	}
	// 事务外仍尝试直接 Enqueue。
	if d.vector.enqueueCnt != 1 {
		t.Errorf("期望 Enqueue 调用 1 次，实际 %d", d.vector.enqueueCnt)
	}
}

func TestArticleService_Update_MetadataOnly_NoOutbox(t *testing.T) {
	deptID := int64(10)
	article := &entity.Article{
		ID:           42,
		Status:       constants.ArticleStatusPublished,
		AuthorID:     1,
		DepartmentID: &deptID,
		Content:      "same content",
		ContentHash:  "same_hash",
	}
	d := buildSvcWithOutbox(article)
	actor := Actor{UserID: 1, Role: constants.RoleDoctor, DeptID: 10}

	newTitle := "new title"
	_, err := d.svc.Update(context.Background(), UpdateInput{
		Title:     &newTitle,
		ArticleID: 42,
		Actor:     actor,
	})
	if err != nil {
		t.Fatalf("Update 返回错误: %v", err)
	}
	// 仅元数据变更，不应写 outbox。
	if d.outbox.insertCall != 0 {
		t.Fatalf("期望 outbox.Insert 不被调用，实际 %d 次", d.outbox.insertCall)
	}
	if d.vector.enqueueCnt != 0 {
		t.Errorf("期望 Enqueue 不被调用，实际 %d 次", d.vector.enqueueCnt)
	}
}

// ============================================================================
// Unarchive：归档恢复应入队向量化重建 chunks
// ============================================================================

func TestArticleService_Unarchive_EnqueuesVectorize(t *testing.T) {
	deptID := int64(10)
	article := &entity.Article{
		ID:           42,
		Status:       constants.ArticleStatusArchived,
		AuthorID:     1,
		DepartmentID: &deptID,
	}
	d := buildSvcWithOutbox(article)
	actor := Actor{UserID: 1, Role: constants.RoleSuperAdmin}

	if err := d.svc.Unarchive(context.Background(), 42, actor); err != nil {
		t.Fatalf("Unarchive 返回错误: %v", err)
	}
	if !d.tx.called {
		t.Error("期望 WithTx 被调用")
	}
	if d.audit.createCnt != 1 {
		t.Errorf("期望审计写入 1 条，实际 %d", d.audit.createCnt)
	}
	// 核心断言：恢复归档文章应事务内写 outbox，保证向量化重建。
	if d.outbox.insertCall != 1 {
		t.Fatalf("期望 outbox.Insert 调用 1 次，实际 %d", d.outbox.insertCall)
	}
	if d.outbox.insertID != 42 {
		t.Errorf("期望 outbox.Insert(42)，实际 %d", d.outbox.insertID)
	}
	// 事务外仍尝试直接 Enqueue。
	if d.vector.enqueueCnt != 1 {
		t.Errorf("期望 Enqueue 调用 1 次，实际 %d", d.vector.enqueueCnt)
	}
}

func TestArticleService_Unarchive_NotArchived_ReturnsConflict(t *testing.T) {
	article := &entity.Article{
		ID:       42,
		Status:   constants.ArticleStatusPublished,
		AuthorID: 1,
	}
	d := buildSvcWithOutbox(article)
	actor := Actor{UserID: 1, Role: constants.RoleSuperAdmin}

	err := d.svc.Unarchive(context.Background(), 42, actor)
	if err == nil {
		t.Fatal("期望 Unarchive 返回错误（非归档状态）")
	}
}

func TestArticleService_Unarchive_NonAdmin_ReturnsForbidden(t *testing.T) {
	deptID := int64(10)
	article := &entity.Article{
		ID:           42,
		Status:       constants.ArticleStatusArchived,
		AuthorID:     1,
		DepartmentID: &deptID,
	}
	d := buildSvcWithOutbox(article)
	actor := Actor{UserID: 1, Role: constants.RoleDoctor, DeptID: 10}

	err := d.svc.Unarchive(context.Background(), 42, actor)
	if err == nil {
		t.Fatal("期望 Unarchive 返回错误（非管理员）")
	}
}

// ============================================================================
// Outbox Insert 失败应导致事务回滚
// ============================================================================

func TestArticleService_Approve_OutboxInsertFails_RollsBack(t *testing.T) {
	deptID := int64(10)
	article := &entity.Article{
		ID:           42,
		Status:       constants.ArticleStatusPending,
		AuthorID:     1,
		DepartmentID: &deptID,
	}
	d := buildSvcWithOutbox(article)
	d.outbox.insertErr = errors.New("outbox db error")
	reviewer := Actor{UserID: 2, Role: constants.RoleDeptAdmin, DeptID: 10}

	err := d.svc.Approve(context.Background(), ApproveInput{
		ArticleID: 42,
		Actor:     reviewer,
	})
	if err == nil {
		t.Fatal("期望 Approve 返回错误（outbox 写入失败应中断事务）")
	}
}

// ============================================================================
// 审核权限规则：仅管理员可审核，管理员可自审
// ============================================================================

func TestArticleService_Approve_SuperAdmin_CanReviewOwnArticle(t *testing.T) {
	deptID := int64(10)
	article := &entity.Article{
		ID:           42,
		Status:       constants.ArticleStatusPending,
		AuthorID:     1, // 作者与审核人相同
		DepartmentID: &deptID,
	}
	d := buildSvcWithOutbox(article)
	actor := Actor{UserID: 1, Role: constants.RoleSuperAdmin, DeptID: 10}

	if err := d.svc.Approve(context.Background(), ApproveInput{ArticleID: 42, Actor: actor}); err != nil {
		t.Fatalf("超级管理员应可审核自己的文章，实际错误: %v", err)
	}
}

func TestArticleService_Approve_SuperAdmin_CanReviewAnyDept(t *testing.T) {
	otherDept := int64(99)
	article := &entity.Article{
		ID:           42,
		Status:       constants.ArticleStatusPending,
		AuthorID:     2,
		DepartmentID: &otherDept, // 不同科室
	}
	d := buildSvcWithOutbox(article)
	actor := Actor{UserID: 1, Role: constants.RoleSuperAdmin, DeptID: 10}

	if err := d.svc.Approve(context.Background(), ApproveInput{ArticleID: 42, Actor: actor}); err != nil {
		t.Fatalf("超级管理员应可审核任意科室文章，实际错误: %v", err)
	}
}

func TestArticleService_Approve_DeptAdmin_CanReviewOwnArticle(t *testing.T) {
	deptID := int64(10)
	article := &entity.Article{
		ID:           42,
		Status:       constants.ArticleStatusPending,
		AuthorID:     1, // 作者与审核人相同
		DepartmentID: &deptID,
	}
	d := buildSvcWithOutbox(article)
	actor := Actor{UserID: 1, Role: constants.RoleDeptAdmin, DeptID: 10}

	if err := d.svc.Approve(context.Background(), ApproveInput{ArticleID: 42, Actor: actor}); err != nil {
		t.Fatalf("科室管理员应可审核自己的文章，实际错误: %v", err)
	}
}

func TestArticleService_Approve_DeptAdmin_CanReviewSameDept(t *testing.T) {
	deptID := int64(10)
	article := &entity.Article{
		ID:           42,
		Status:       constants.ArticleStatusPending,
		AuthorID:     2,
		DepartmentID: &deptID,
	}
	d := buildSvcWithOutbox(article)
	actor := Actor{UserID: 1, Role: constants.RoleDeptAdmin, DeptID: 10}

	if err := d.svc.Approve(context.Background(), ApproveInput{ArticleID: 42, Actor: actor}); err != nil {
		t.Fatalf("科室管理员应可审核本科室文章，实际错误: %v", err)
	}
}

func TestArticleService_Approve_DeptAdmin_CannotReviewOtherDept(t *testing.T) {
	otherDept := int64(99)
	article := &entity.Article{
		ID:           42,
		Status:       constants.ArticleStatusPending,
		AuthorID:     2,
		DepartmentID: &otherDept,
	}
	d := buildSvcWithOutbox(article)
	actor := Actor{UserID: 1, Role: constants.RoleDeptAdmin, DeptID: 10}

	err := d.svc.Approve(context.Background(), ApproveInput{ArticleID: 42, Actor: actor})
	if err == nil {
		t.Fatal("科室管理员不应审核其他科室文章")
	}
}

func TestArticleService_Approve_DoctorCannotReview(t *testing.T) {
	deptID := int64(10)
	article := &entity.Article{
		ID:           42,
		Status:       constants.ArticleStatusPending,
		AuthorID:     2,
		DepartmentID: &deptID,
	}
	d := buildSvcWithOutbox(article)
	actor := Actor{UserID: 1, Role: constants.RoleDoctor, DeptID: 10}

	err := d.svc.Approve(context.Background(), ApproveInput{ArticleID: 42, Actor: actor})
	if err == nil {
		t.Fatal("非管理员不应审核文章")
	}
}

func TestArticleService_Reject_SuperAdmin_CanRejectOwnArticle(t *testing.T) {
	deptID := int64(10)
	article := &entity.Article{
		ID:           42,
		Status:       constants.ArticleStatusPending,
		AuthorID:     1,
		DepartmentID: &deptID,
	}
	d := buildSvcWithOutbox(article)
	actor := Actor{UserID: 1, Role: constants.RoleSuperAdmin, DeptID: 10}

	if err := d.svc.Reject(context.Background(), RejectInput{ArticleID: 42, Reason: "不合规", Actor: actor}); err != nil {
		t.Fatalf("超级管理员应可驳回自己的文章，实际错误: %v", err)
	}
}

func TestArticleService_Reject_DoctorCannotReject(t *testing.T) {
	deptID := int64(10)
	article := &entity.Article{
		ID:           42,
		Status:       constants.ArticleStatusPending,
		AuthorID:     2,
		DepartmentID: &deptID,
	}
	d := buildSvcWithOutbox(article)
	actor := Actor{UserID: 1, Role: constants.RoleDoctor, DeptID: 10}

	err := d.svc.Reject(context.Background(), RejectInput{ArticleID: 42, Reason: "不合规", Actor: actor})
	if err == nil {
		t.Fatal("非管理员不应驳回文章")
	}
}

func TestArticleService_SetFeaturedRank(t *testing.T) {
	deptID := int64(10)
	article := &entity.Article{ID: 42, Status: constants.ArticleStatusPublished, DepartmentID: &deptID}
	svc, repo, audit, _, tx := buildSvc(article)
	actor := Actor{UserID: 2, Role: constants.RoleDeptAdmin, DeptID: deptID}

	if err := svc.SetFeaturedRank(context.Background(), article.ID, 1, actor); err != nil {
		t.Fatalf("SetFeaturedRank 返回错误: %v", err)
	}
	if !tx.called || repo.featuredID != article.ID || repo.featuredRank != 1 || audit.createCnt != 1 {
		t.Fatalf("热门设置未完整执行: tx=%t id=%d rank=%d audit=%d", tx.called, repo.featuredID, repo.featuredRank, audit.createCnt)
	}
}

func TestArticleService_SetFeaturedRank_RejectsUnauthorizedOrUnpublished(t *testing.T) {
	deptID := int64(10)
	article := &entity.Article{ID: 42, Status: constants.ArticleStatusDraft, DepartmentID: &deptID}
	svc, _, _, _, _ := buildSvc(article)
	admin := Actor{UserID: 2, Role: constants.RoleDeptAdmin, DeptID: deptID}
	if err := svc.SetFeaturedRank(context.Background(), article.ID, 1, admin); err == nil {
		t.Fatal("未发布文章设置热门应失败")
	}
	article.Status = constants.ArticleStatusPublished
	if err := svc.SetFeaturedRank(context.Background(), article.ID, 1, Actor{UserID: 3, Role: constants.RoleDoctor, DeptID: deptID}); err == nil {
		t.Fatal("非管理员设置热门应失败")
	}
}

// ============================================================================
// Create 输入校验回归（EDGE-ART-006/007）：
// 纯空白 content / 空白 title 应 422 拒绝；超长 title 应 422 而非落库触发 DB 500。
// ============================================================================

func assertValidationCode(t *testing.T, err error, wantCode string) {
	t.Helper()
	if err == nil {
		t.Fatalf("期望错误码 %s，实际 nil", wantCode)
	}
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("期望 AppError，实际 %T: %v", err, err)
	}
	if appErr.Code != wantCode {
		t.Errorf("期望错误码 %s，实际 %s", wantCode, appErr.Code)
	}
	if appErr.HTTP != http.StatusUnprocessableEntity {
		t.Errorf("期望 HTTP 422，实际 %d", appErr.HTTP)
	}
}

func TestArticleService_Create_WhitespaceContent_Rejected(t *testing.T) {
	svc, _, _, _, _ := buildSvc(nil)
	_, err := svc.Create(context.Background(), CreateInput{
		Title:        "有效标题",
		Content:      "   \n\t  ",
		DepartmentID: 4,
		Actor:        Actor{UserID: 1, Role: constants.RoleDoctor, DeptID: 4},
	})
	assertValidationCode(t, err, "WIKI_CONTENT_REQUIRED")
}

func TestArticleService_Create_BlankTitle_Rejected(t *testing.T) {
	svc, _, _, _, _ := buildSvc(nil)
	_, err := svc.Create(context.Background(), CreateInput{
		Title:        "   ",
		Content:      "有效内容",
		DepartmentID: 4,
		Actor:        Actor{UserID: 1, Role: constants.RoleDoctor, DeptID: 4},
	})
	assertValidationCode(t, err, "WIKI_TITLE_REQUIRED")
}

func TestArticleService_Create_TitleTooLong_Rejected(t *testing.T) {
	svc, _, _, _, _ := buildSvc(nil)
	_, err := svc.Create(context.Background(), CreateInput{
		Title:        strings.Repeat("T", titleMaxRunes+1),
		Content:      "有效内容",
		DepartmentID: 4,
		Actor:        Actor{UserID: 1, Role: constants.RoleDoctor, DeptID: 4},
	})
	assertValidationCode(t, err, "WIKI_TITLE_TOO_LONG")
}
