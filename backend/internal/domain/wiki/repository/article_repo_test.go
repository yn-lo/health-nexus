// Package repository 文章仓储单测：聚焦 UPDATE 语句生成的状态守卫（无需真实数据库）。
package repository

import (
	"strings"
	"testing"

	"health-nexus/internal/shared/constants"
)

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
	wantVersion := "version = CASE WHEN status IN ('" + constants.ArticleStatusPublished +
		"','" + constants.ArticleStatusPending + "') THEN version + 1 ELSE version END"
	if !strings.Contains(got, wantVersion) {
		t.Errorf("内容变更未按行当前状态递增版本（pending 编辑也须递增，否则旧审批仍能通过）：\n%s", got)
	}
}

// TestApplyUpdateFields_PendingEditBumpsVersion P1：待审核（pending）期间修改内容同样递增版本——
// pending 编辑不递增会让审批的版本校验无法发现内容变化，旧审批仍能批准未审阅的新内容。
func TestApplyUpdateFields_PendingEditBumpsVersion(t *testing.T) {
	hash := "new-hash"
	b := &updateBuilder{}
	b.applyUpdateFields(UpdateFields{ContentHash: &hash, ReReviewOnContentChange: true})
	got := strings.Join(b.sets, ", ")

	// pending 必须落在递增版本的集合内。
	if !strings.Contains(got, "'"+constants.ArticleStatusPending+"') THEN version + 1") {
		t.Errorf("pending 内容变更未递增版本（审批版本校验失效）：\n%s", got)
	}
	// pending 的 status 不得被改写（仍为 pending，重新审核语义不变）。
	wantStatus := "status = CASE WHEN status = '" + constants.ArticleStatusPublished +
		"' THEN '" + constants.ArticleStatusPending + "' ELSE status END"
	if !strings.Contains(got, wantStatus) {
		t.Errorf("pending 状态不应被改写：\n%s", got)
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
