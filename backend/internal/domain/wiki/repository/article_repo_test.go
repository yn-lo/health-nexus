// Package repository 文章仓储单测：聚焦 UPDATE 语句生成的状态守卫（无需真实数据库）。
package repository

import (
	"strings"
	"testing"

	"health-nexus/internal/shared/constants"
)

// TestOnlineDepartmentSQL_ExcludesInactiveDepartments 科室禁用 = 整体知识库下线：
// 公开列表/精选/详情与 RAG 检索共用的可见性条件必须排除所属科室已禁用的文章，
// 同时保留 department_id 为空的全院公共知识（schema 允许 NULL）。
func TestOnlineDepartmentSQL_ExcludesInactiveDepartments(t *testing.T) {
	withAlias := onlineDepartmentSQL("a")
	for _, want := range []string{
		"a.department_id IS NULL",
		"FROM departments d",
		"d.is_active = false",
	} {
		if !strings.Contains(withAlias, want) {
			t.Errorf("科室在线条件缺少 %q：\n%s", want, withAlias)
		}
	}

	noAlias := onlineDepartmentSQL("")
	if !strings.Contains(noAlias, "department_id IS NULL") {
		t.Errorf("无别名（UPDATE 语句）未兼容 department_id 为空：\n%s", noAlias)
	}
	if strings.Contains(noAlias, "a.department_id") {
		t.Errorf("无别名场景不得带上别名 a.：\n%s", noAlias)
	}
}

// TestApplyUpdateFields_ContentChangeGuardsReReview P1：内容变更时，状态回退重新审核的判定
// 必须写在 UPDATE 语句内、按行**当前**状态求值（CASE），不能依赖服务端读取时的状态快照——
// 否则"作者读到 pending、管理员随后审核发布、作者的写才提交"会让未审核的新正文保持
// published 并进入向量化队列（并发编辑绕过审核）。
func TestApplyUpdateFields_ContentChangeGuardsReReview(t *testing.T) {
	hash := "new-hash"
	b := &updateBuilder{}
	b.applyUpdateFields(UpdateFields{ContentHash: &hash, ReReviewOnContentChange: true})
	got := strings.Join(b.sets, ", ")

	wantStatus := "status = CASE WHEN status = '" + constants.ArticleStatusPublished +
		"' THEN '" + constants.ArticleStatusPending + "' ELSE status END"
	if !strings.Contains(got, wantStatus) {
		t.Errorf("内容变更未按行当前状态回退 pending（并发可绕过审核）：\n%s", got)
	}
	// 版本号是编辑修订号：任何状态（含 draft/pending）都必须递增，否则草稿的乐观锁形同虚设、
	// 旧页面保存会静默覆盖新内容，pending 编辑也无法被审批版本校验发现（P2 修复）。
	if !strings.Contains(got, "version = version + 1") {
		t.Errorf("内容变更未递增版本（草稿乐观锁失效 / 旧审批仍能通过）：\n%s", got)
	}
}

// TestApplyUpdateFields_PendingEditBumpsVersion P1：待审核（pending）期间修改内容同样递增版本——
// pending 编辑不递增会让审批的版本校验无法发现内容变化，旧审批仍能批准未审阅的新内容。
func TestApplyUpdateFields_PendingEditBumpsVersion(t *testing.T) {
	hash := "new-hash"
	b := &updateBuilder{}
	b.applyUpdateFields(UpdateFields{ContentHash: &hash, ReReviewOnContentChange: true})
	got := strings.Join(b.sets, ", ")

	// pending 与其它任何状态一样递增版本。
	if !strings.Contains(got, "version = version + 1") {
		t.Errorf("pending 内容变更未递增版本（审批版本校验失效）：\n%s", got)
	}
	// pending 的 status 不得被改写（仍为 pending，重新审核语义不变）。
	wantStatus := "status = CASE WHEN status = '" + constants.ArticleStatusPublished +
		"' THEN '" + constants.ArticleStatusPending + "' ELSE status END"
	if !strings.Contains(got, wantStatus) {
		t.Errorf("pending 状态不应被改写：\n%s", got)
	}
}

// TestApplyUpdateFields_DraftEditBumpsVersion P2：草稿（draft）编辑内容同样递增版本——
// 原 SQL 只对 published/pending 递增，草稿 version 恒定，"旧页面保存"总能通过 version 守卫
// 而静默覆盖新内容（乐观锁形同虚设）。
func TestApplyUpdateFields_DraftEditBumpsVersion(t *testing.T) {
	hash := "new-hash"
	b := &updateBuilder{}
	b.applyUpdateFields(UpdateFields{ContentHash: &hash, ReReviewOnContentChange: true})
	got := strings.Join(b.sets, ", ")

	// 版本递增不得再以行状态为条件（draft 也必须递增）。
	if strings.Contains(got, "CASE WHEN status IN") {
		t.Errorf("版本递增仍是条件式（draft 编辑不会递增，乐观锁失效）：\n%s", got)
	}
	if !strings.Contains(got, "version = version + 1") {
		t.Errorf("draft 内容变更未递增版本（乐观锁失效）：\n%s", got)
	}
}

// TestApplyUpdateFields_MetadataOnly_NoReReviewGuard 仅元数据变更（无内容变更）不得触发
// 状态回退——否则标题/封面的编辑也会把文章打回待审核。
func TestApplyUpdateFields_MetadataOnly_NoReReviewGuard(t *testing.T) {
	title := "new title"
	b := &updateBuilder{}
	b.applyUpdateFields(UpdateFields{Title: &title, IncrementVersion: true})
	got := strings.Join(b.sets, ", ")

	if strings.Contains(got, "status = CASE") {
		t.Errorf("元数据变更不应回退状态：\n%s", got)
	}
	if !strings.Contains(got, "version = version + 1") {
		t.Errorf("已发布文章元数据变更应递增版本：\n%s", got)
	}
}

// TestArticleScanCols_MatchesArticleColumns articleColumns 与 articleScanCols 必须逐列对应——
// 二者错位会让所有 SELECT 扫描失败（列数/类型不匹配），是本类改动最易犯的错（P1 新增两列）。
func TestArticleScanCols_MatchesArticleColumns(t *testing.T) {
	// 把别名前缀 i. 去掉后应与 articleColumns 完全一致。
	got := strings.NewReplacer("i.", "", "\n", " ", "\t", " ").Replace(articleScanCols)
	got = strings.Join(strings.Fields(got), " ")
	want := strings.Join(strings.Fields(strings.ReplaceAll(articleColumns, "\n", " ")), " ")
	if got != want {
		t.Errorf("扫描列与选列不对应：\n got=%s\nwant=%s", got, want)
	}
}

// TestApplyUpdateFields_ContentChangeKeepsPublishedSnapshot 内容变更只改编辑稿 content，
// 不得触碰 published_content——否则患者端的已审核快照会被未审核新稿覆盖（P1）。
func TestApplyUpdateFields_ContentChangeKeepsPublishedSnapshot(t *testing.T) {
	hash := "new-hash"
	content := "new draft content"
	b := &updateBuilder{}
	b.applyUpdateFields(UpdateFields{
		Content: &content, ContentHash: &hash, ReReviewOnContentChange: true,
	})
	got := strings.Join(b.sets, ", ")
	if strings.Contains(got, "published_content") {
		t.Errorf("内容变更不得改写已发布快照 published_content：\n%s", got)
	}
}

// TestPublishSnapshotSets_CoversPatientVisibleFields P1：发布快照必须覆盖患者可见与检索/审核策略
// 依赖的全部字段。只固化正文会让作者改标题/摘要立即对患者生效，或降 content_risk 提前放宽审核策略。
func TestPublishSnapshotSets_CoversPatientVisibleFields(t *testing.T) {
	joined := strings.Join(publishSnapshotSets, ", ")
	for _, want := range []string{
		"published_content = content",
		"published_content_hash = content_hash",
		"published_version = version",
		"published_title = title",
		"published_summary = summary",
		"published_content_risk = content_risk",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("发布快照缺少 %q（患者可见/安全字段未纳入审核版本）：\n%s", want, joined)
		}
	}
}

// TestArticleColumns_HasPublishedSnapshotColumns 文章列必须包含快照的标题/摘要/风险等级，
// 否则 GetPublishedByID 无从以已审核版本覆盖当前编辑稿（P1 绕过审核）。
func TestArticleColumns_HasPublishedSnapshotColumns(t *testing.T) {
	for _, col := range []string{"published_title", "published_summary", "published_content_risk"} {
		if !strings.Contains(articleColumns, col) {
			t.Errorf("articleColumns 缺少快照列 %q", col)
		}
		if !strings.Contains(articleScanCols, "i."+col) {
			t.Errorf("articleScanCols 缺少快照列 i.%s", col)
		}
	}
}

// TestChunkSearchColumns_UsesPublishedRisk P1：检索必须读已审核快照的风险等级。
// 若读当前编辑稿 a.content_risk，作者把 risk 从 high 降为 normal（待审核）后，
// 检索可见性与答案审核策略会提前按未审核值生效。
func TestChunkSearchColumns_UsesPublishedRisk(t *testing.T) {
	if !strings.Contains(chunkSearchColumns, "a.published_content_risk") {
		t.Errorf("检索列未读取已审核风险等级 published_content_risk：\n%s", chunkSearchColumns)
	}
	if strings.Contains(chunkSearchColumns, "a.content_risk") {
		t.Errorf("检索列仍在读当前编辑稿 a.content_risk（可绕过审核）：\n%s", chunkSearchColumns)
	}
}
