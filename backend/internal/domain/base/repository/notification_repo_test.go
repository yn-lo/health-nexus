// Package repository 通知仓储占位符一致性测试（无需真实数据库）。
package repository

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var placeholderRe = regexp.MustCompile(`\$(\d+)`)

// maxPlaceholder 返回 SQL 中最大占位符序号。
func maxPlaceholder(t *testing.T, sql string) int {
	t.Helper()
	highest := 0
	for _, m := range placeholderRe.FindAllStringSubmatch(sql, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatalf("解析占位符 %q: %v", m[0], err)
		}
		if n > highest {
			highest = n
		}
	}
	return highest
}

// TestNotificationQueries_PlaceholderIndexMatchesArgs P2：科室参数必须落在 $2，且与调用方
// 传入的参数个数一致。此前 deptVisibility 固定写 $2，而 MarkRead 的 $2 是角色——角色的字符串
// 被当 BIGINT 使用、第三个参数无处可放，该接口必然执行失败。
// 断言：SQL 最大占位符序号 == 参数个数；科室条件在 $2；角色占位符与参数布局一致。
func TestNotificationQueries_PlaceholderIndexMatchesArgs(t *testing.T) {
	cases := []struct {
		name      string
		sql       string
		argCount  int
		deptInSQL string
		roleInSQL string
	}{
		{"ListForRole", listForRoleQuery, 3, "$2::BIGINT", "recipient_role = $1"},
		{"MarkRead", markReadQuery, 3, "$2::BIGINT", "recipient_role = $3"},
		{"MarkAllRead", markAllReadQuery, 2, "$2::BIGINT", "recipient_role = $1"},
		{"UnreadCount", unreadCountQuery, 2, "$2::BIGINT", "recipient_role = $1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := maxPlaceholder(t, tc.sql); got != tc.argCount {
				t.Errorf("%s 最大占位符 = $%d，期望 $%d（须与传入参数个数一致）：\n%s",
					tc.name, got, tc.argCount, tc.sql)
			}
			if !strings.Contains(tc.sql, tc.deptInSQL) {
				t.Errorf("%s 科室条件未使用 %s（占位符错位会把角色当科室）：\n%s",
					tc.name, tc.deptInSQL, tc.sql)
			}
			if !strings.Contains(tc.sql, tc.roleInSQL) {
				t.Errorf("%s 角色条件未使用 %s（与科室占位符冲突）：\n%s",
					tc.name, tc.roleInSQL, tc.sql)
			}
		})
	}
}
