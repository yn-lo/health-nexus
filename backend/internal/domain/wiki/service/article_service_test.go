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
	// 落库捕获：Create 的实体 / UpdateFields 的待更新字段，便于断言写入前已被规范化。
	created      *entity.Article
	updateFields repository.UpdateFields
	// UpdateStatus 落库捕获：便于断言审核通过时清除了复审逾期标记（P2）。
	updateStatusOpts repository.StatusUpdateOpts
}

func (m *mockArticleRepo) Create(_ context.Context, a *entity.Article) error {
	m.created = a
	return nil
}
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
func (m *mockArticleRepo) UpdateFields(_ context.Context, _ int64, f repository.UpdateFields) (*entity.Article, error) {
	m.updateFields = f
	// 模拟真实 SQL 更新：应用内容/元数据变化，并按行当前状态处理重新审核（P1）。
	// 注意：真实 UPDATE 会改写行本身，故此处同步回写 m.article（供后续 GetByID 读到新版本）。
	updated := *m.article
	if f.Content != nil {
		updated.Content = *f.Content
	}
	if f.ContentHash != nil {
		updated.ContentHash = *f.ContentHash
	}
	if f.ReReviewOnContentChange {
		switch updated.Status {
		case constants.ArticleStatusPublished:
			// 内容变更且行当前为 published → 回退待审核 + 版本递增（与 SQL CASE 一致）。
			updated.Status = constants.ArticleStatusPending
			updated.Version++
		case constants.ArticleStatusPending:
			// pending 期间内容变更：状态不变，但同样递增版本（P1，与 SQL CASE 一致）。
			updated.Version++
		}
	} else if f.IncrementVersion {
		updated.Version++
	}
	*m.article = updated
	return &updated, nil
}
func (m *mockArticleRepo) UpdateStatus(_ context.Context, _ int64, fromStatus, toStatus string, opts repository.StatusUpdateOpts) error {
	m.updateStatusOpts = opts
	if m.updateStatErr != nil {
		return m.updateStatErr
	}
	// 模拟条件更新：状态不匹配或审批版本漂移时不命中，返回 ErrStatusConflict（P1）。
	if m.article == nil || m.article.Status != fromStatus {
		return repository.ErrStatusConflict
	}
	if opts.ExpectedVersion != nil && m.article.Version != *opts.ExpectedVersion {
		return repository.ErrStatusConflict
	}
	m.article.Status = toStatus
	return nil
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
// 文章 version 未显式设置时补为 1（审批版本守卫要求 ExpectedVersion>0，多数用例默认审阅 v1）。
func buildSvcWithOutbox(article *entity.Article) svcDeps {
	if article != nil && article.Version == 0 {
		article.Version = 1
	}
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
		ArticleID:       42,
		Actor:           reviewer,
		ExpectedVersion: 1,
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
		ArticleID:       42,
		Actor:           reviewer,
		ExpectedVersion: 1,
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

// TestArticleService_Update_PublishedContentChange_ReturnsToPendingReview 已发布文章的内容修改
// 必须回到待审核（P1）：不失效原切片、不写 outbox、不触发重新向量化——
// 审核通过前检索继续服务上一次审核通过的版本，避免未审核内容进入患者可见的知识库。
func TestArticleService_Update_PublishedContentChange_ReturnsToPendingReview(t *testing.T) {
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
	dto, err := d.svc.Update(context.Background(), UpdateInput{
		Content:   &newContent,
		ArticleID: 42,
		Actor:     actor,
	})
	if err != nil {
		t.Fatalf("Update 返回错误: %v", err)
	}
	// 状态回到待审核，且写入字段显式要求"内容变更回退重新审核"。
	if dto.Status != constants.ArticleStatusPending {
		t.Errorf("更新后状态 = %q，期望 %q（需重新审核）", dto.Status, constants.ArticleStatusPending)
	}
	if !d.repo.updateFields.ReReviewOnContentChange {
		t.Error("期望写入 ReReviewOnContentChange=true（由 SQL 按当前状态回退 pending）")
	}
	// 审核通过前不得动旧切片、不得重建向量。
	if d.chunks.deactivateCall != 0 {
		t.Errorf("待重新审核期间不得失效原切片，实际调用 %d 次", d.chunks.deactivateCall)
	}
	if d.outbox.insertCall != 0 {
		t.Errorf("待重新审核期间不得写 outbox，实际 %d 次", d.outbox.insertCall)
	}
	if d.vector.enqueueCnt != 0 {
		t.Errorf("待重新审核期间不得入队向量化，实际 %d 次", d.vector.enqueueCnt)
	}
	// 审计日志须记录真实的 published → pending 迁移。
	if d.audit.createCnt != 1 {
		t.Errorf("期望审计写入 1 条，实际 %d", d.audit.createCnt)
	}
}

// TestArticleService_Update_ContentChangeEvenIfStalePending_CarriesReReviewGuard P1 并发回归：
// 作者读到 pending（随后管理员审核发布），内容变更仍必须携带 ReReviewOnContentChange——
// 由 SQL 按行当前状态回退 pending，避免新正文保持已发布并进入向量化队列。
func TestArticleService_Update_ContentChangeEvenIfStalePending_CarriesReReviewGuard(t *testing.T) {
	deptID := int64(10)
	article := &entity.Article{
		ID:           42,
		Status:       constants.ArticleStatusPending, // 写入时的实际状态可能已变为 published
		AuthorID:     1,
		DepartmentID: &deptID,
		Content:      "old content",
		ContentHash:  "old_hash",
	}
	d := buildSvcWithOutbox(article)
	actor := Actor{UserID: 1, Role: constants.RoleDoctor, DeptID: 10}

	newContent := "new content"
	if _, err := d.svc.Update(context.Background(), UpdateInput{
		Content:   &newContent,
		ArticleID: 42,
		Actor:     actor,
	}); err != nil {
		t.Fatalf("Update 返回错误: %v", err)
	}
	if !d.repo.updateFields.ReReviewOnContentChange {
		t.Error("内容变更必须要求 SQL 内重新审核判定，不能依赖读取时的状态快照")
	}
	// 待重新审核期间不得失效原切片、不得重建向量。
	if d.chunks.deactivateCall != 0 || d.outbox.insertCall != 0 || d.vector.enqueueCnt != 0 {
		t.Errorf("待重新审核期间不得动切片/向量化：deactivate=%d outbox=%d enqueue=%d",
			d.chunks.deactivateCall, d.outbox.insertCall, d.vector.enqueueCnt)
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

// RED→GREEN：更新接口提供的 summary 含 HTML 实体时，写入前应规范化为纯文本。
func TestArticleService_Update_ProvidedSummary_NormalizesHTMLEntities(t *testing.T) {
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

	raw := "规则：&quot;安全第一&quot;"
	_, err := d.svc.Update(context.Background(), UpdateInput{
		Summary:   &raw,
		ArticleID: 42,
		Actor:     actor,
	})
	if err != nil {
		t.Fatalf("Update 返回错误: %v", err)
	}
	if d.repo.updateFields.Summary == nil {
		t.Fatal("期望 UpdateFields 携带被规范化的 Summary")
	}
	want := `规则："安全第一"`
	if got := *d.repo.updateFields.Summary; got != want {
		t.Errorf("期望 summary 写入前被反转义为 %q，实际 %q", want, got)
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

// TestArticleService_Unarchive_DeptAdmin_CannotCrossDept P1：科室管理员不得恢复其他科室的归档文章
// （否则他人科室内容会被重新公开并进入向量化队列）。
func TestArticleService_Unarchive_DeptAdmin_CannotCrossDept(t *testing.T) {
	otherDept := int64(99)
	article := &entity.Article{
		ID:           42,
		Status:       constants.ArticleStatusArchived,
		AuthorID:     1,
		DepartmentID: &otherDept,
	}
	d := buildSvcWithOutbox(article)
	actor := Actor{UserID: 1, Role: constants.RoleDeptAdmin, DeptID: 10}

	err := d.svc.Unarchive(context.Background(), 42, actor)
	if err == nil {
		t.Fatal("科室管理员不应恢复其他科室的归档文章")
	}
	assertAppErrCode(t, err, "WIKI_DEPT_MISMATCH")
	// 越权被拒后不得产生任何副作用。
	if d.outbox.insertCall != 0 || d.vector.enqueueCnt != 0 || d.audit.createCnt != 0 {
		t.Errorf("越权拒绝后不应有副作用：outbox=%d enqueue=%d audit=%d",
			d.outbox.insertCall, d.vector.enqueueCnt, d.audit.createCnt)
	}
}

// TestArticleService_Unarchive_DeptAdmin_SameDept_Succeeds 本科室管理员恢复本科室归档文章应成功。
func TestArticleService_Unarchive_DeptAdmin_SameDept_Succeeds(t *testing.T) {
	deptID := int64(10)
	article := &entity.Article{
		ID:           42,
		Status:       constants.ArticleStatusArchived,
		AuthorID:     1,
		DepartmentID: &deptID,
	}
	d := buildSvcWithOutbox(article)
	actor := Actor{UserID: 1, Role: constants.RoleDeptAdmin, DeptID: 10}

	if err := d.svc.Unarchive(context.Background(), 42, actor); err != nil {
		t.Fatalf("本科室管理员应可恢复本科室归档文章，实际错误: %v", err)
	}
	if d.outbox.insertCall != 1 {
		t.Errorf("恢复归档应写 outbox，实际 %d 次", d.outbox.insertCall)
	}
}

// assertAppErrCode 断言 err 为 *AppError 且错误码匹配。
func assertAppErrCode(t *testing.T, err error, wantCode string) {
	t.Helper()
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("期望 *AppError，实际 %T: %v", err, err)
	}
	if appErr.Code != wantCode {
		t.Errorf("期望错误码 %s，实际 %s", wantCode, appErr.Code)
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
		ArticleID:       42,
		Actor:           reviewer,
		ExpectedVersion: 1,
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

	if err := d.svc.Approve(context.Background(), ApproveInput{ArticleID: 42, Actor: actor, ExpectedVersion: 1}); err != nil {
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

	if err := d.svc.Approve(context.Background(), ApproveInput{ArticleID: 42, Actor: actor, ExpectedVersion: 1}); err != nil {
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

	if err := d.svc.Approve(context.Background(), ApproveInput{ArticleID: 42, Actor: actor, ExpectedVersion: 1}); err != nil {
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

	if err := d.svc.Approve(context.Background(), ApproveInput{ArticleID: 42, Actor: actor, ExpectedVersion: 1}); err != nil {
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

	err := d.svc.Approve(context.Background(), ApproveInput{ArticleID: 42, Actor: actor, ExpectedVersion: 1})
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

	err := d.svc.Approve(context.Background(), ApproveInput{ArticleID: 42, Actor: actor, ExpectedVersion: 1})
	if err == nil {
		t.Fatal("非管理员不应审核文章")
	}
}

// TestArticleService_Approve_ClearsReviewOverdue P2：重新审核通过必须清除复审逾期标记，
// 否则高风险逾期文章即使重新发布仍被检索层排除（患者/患者端无法命中）。
func TestArticleService_Approve_ClearsReviewOverdue(t *testing.T) {
	deptID := int64(10)
	article := &entity.Article{
		ID:            42,
		Status:        constants.ArticleStatusPending,
		AuthorID:      1,
		DepartmentID:  &deptID,
		ReviewOverdue: true,
	}
	d := buildSvcWithOutbox(article)
	reviewer := Actor{UserID: 2, Role: constants.RoleDeptAdmin, DeptID: 10}

	if err := d.svc.Approve(context.Background(), ApproveInput{ArticleID: 42, Actor: reviewer, ExpectedVersion: 1}); err != nil {
		t.Fatalf("Approve 返回错误: %v", err)
	}
	if !d.repo.updateStatusOpts.ClearReviewOverdue {
		t.Error("审核通过时应清除 review_overdue 标记（P2）")
	}
}

// TestArticleService_Approve_VersionMismatch_Returns409 P1：审核者审阅版本与当前版本不一致
// （审阅期间被作者改成新内容）必须拒绝，要求重新审阅，不得批准未审阅的版本。
func TestArticleService_Approve_VersionMismatch_Returns409(t *testing.T) {
	deptID := int64(10)
	article := &entity.Article{
		ID:           42,
		Status:       constants.ArticleStatusPending,
		Version:      2, // 审阅后又被编辑，版本已升到 2
		AuthorID:     1,
		DepartmentID: &deptID,
	}
	d := buildSvcWithOutbox(article)
	reviewer := Actor{UserID: 2, Role: constants.RoleDeptAdmin, DeptID: 10}

	err := d.svc.Approve(context.Background(), ApproveInput{
		ArticleID:       42,
		Actor:           reviewer,
		ExpectedVersion: 1, // 审阅者看到的是 v1
	})
	if err == nil {
		t.Fatal("版本不一致时应拒绝审批")
	}
	assertAppErrCode(t, err, "WIKI_REVIEW_VERSION_CONFLICT")
	if d.outbox.insertCall != 0 || d.vector.enqueueCnt != 0 {
		t.Errorf("版本冲突时不得发布/入队：outbox=%d enqueue=%d", d.outbox.insertCall, d.vector.enqueueCnt)
	}
}

// TestArticleService_Approve_MissingVersion_Returns422 缺少审阅版本号时必须拒绝（强制客户端带版本）。
func TestArticleService_Approve_MissingVersion_Returns422(t *testing.T) {
	deptID := int64(10)
	article := &entity.Article{
		ID: 42, Status: constants.ArticleStatusPending, AuthorID: 1, DepartmentID: &deptID,
	}
	d := buildSvcWithOutbox(article)
	reviewer := Actor{UserID: 2, Role: constants.RoleDeptAdmin, DeptID: 10}

	err := d.svc.Approve(context.Background(), ApproveInput{ArticleID: 42, Actor: reviewer})
	if err == nil {
		t.Fatal("缺少审阅版本时应拒绝")
	}
	assertAppErrCode(t, err, "WIKI_REVIEW_VERSION_REQUIRED")
}

// TestArticleService_PendingEditThenApprove_StaleVersionRejected P1 端到端：
// 待审核（pending）期间作者改了正文（版本递增），管理员基于旧版本审批必须被拒——
// 否则"pending 编辑不递增版本"会让审批的版本校验形同虚设，旧审批仍能批准未审阅的新内容。
func TestArticleService_PendingEditThenApprove_StaleVersionRejected(t *testing.T) {
	deptID := int64(10)
	article := &entity.Article{
		ID:           42,
		Status:       constants.ArticleStatusPending,
		Version:      1, // 管理员审阅的是 v1
		AuthorID:     1,
		DepartmentID: &deptID,
		Content:      "旧正文",
		ContentHash:  "old_hash",
	}
	d := buildSvcWithOutbox(article)
	author := Actor{UserID: 1, Role: constants.RoleDoctor, DeptID: 10}

	// 作者在 pending 期间修改正文 → 版本应递增到 2。
	newContent := "新正文（未审阅）"
	dto, err := d.svc.Update(context.Background(), UpdateInput{
		Content:   &newContent,
		ArticleID: 42,
		Actor:     author,
	})
	if err != nil {
		t.Fatalf("Update 返回错误: %v", err)
	}
	if dto.Version != 2 {
		t.Fatalf("pending 内容变更后版本 = %d，期望 2（须递增）", dto.Version)
	}
	if dto.Status != constants.ArticleStatusPending {
		t.Fatalf("pending 内容变更后状态 = %q，期望仍为 pending", dto.Status)
	}

	// 管理员仍基于审阅时的 v1 审批 → 必须 409，不得批准未审阅的新正文。
	reviewer := Actor{UserID: 2, Role: constants.RoleDeptAdmin, DeptID: 10}
	err = d.svc.Approve(context.Background(), ApproveInput{
		ArticleID:       42,
		Actor:           reviewer,
		ExpectedVersion: 1,
	})
	if err == nil {
		t.Fatal("待审核期间内容已变更，旧版本审批必须被拒绝")
	}
	assertAppErrCode(t, err, "WIKI_REVIEW_VERSION_CONFLICT")
	// 不得发布/入队（未审阅内容不得进入知识库）。
	if d.outbox.insertCall != 0 || d.vector.enqueueCnt != 0 {
		t.Errorf("版本冲突时不得发布/入队：outbox=%d enqueue=%d", d.outbox.insertCall, d.vector.enqueueCnt)
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

// RED→GREEN：客户端提供的 summary 含 HTML 实体时，写入前应规范化为纯文本（反转义 &quot; 等）。
func TestArticleService_Create_ProvidedSummary_NormalizesHTMLEntities(t *testing.T) {
	svc, repo, _, _, _ := buildSvc(nil)
	_, err := svc.Create(context.Background(), CreateInput{
		Title:        "标题",
		Content:      "<p>正文</p>",
		Summary:      "结论是 &quot;最优语言&quot; 没有唯一答案",
		DepartmentID: 4,
		Actor:        Actor{UserID: 1, Role: constants.RoleDoctor, DeptID: 4},
	})
	if err != nil {
		t.Fatalf("Create 返回错误: %v", err)
	}
	if repo.created == nil {
		t.Fatal("期望 Create 落库被捕获")
	}
	want := `结论是 "最优语言" 没有唯一答案`
	if repo.created.Summary != want {
		t.Errorf("期望 summary 写入前被反转义为 %q，实际 %q", want, repo.created.Summary)
	}
}
