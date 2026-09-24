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
	wantVersion := "version = CASE WHEN status = '" + constants.ArticleStatusPublished + "' THEN version + 1 ELSE version END"
	if !strings.Contains(got, wantVersion) {
		t.Errorf("内容变更未按行当前状态递增版本：\n%s", got)
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
