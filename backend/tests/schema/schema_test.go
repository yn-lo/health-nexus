// Package schema_test 校验 Go entity struct 与 DB 表结构的一致性。
// 通过 information_schema 查询实际列，与反射推导的 Go struct 字段对比，
// 任何不匹配（字段增删未同步）都会导致测试失败。
//
// 运行条件：需要可用的 PostgreSQL 实例（与 config.yaml 同 DSN）。
// 跳过方式：不设 SCHEMA_TEST_DSN 且 localhost:5432 不可达时自动 Skip。
package schema_test

import (
	"context"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	authentity "health-nexus/internal/domain/auth/entity"
	baseentity "health-nexus/internal/domain/base/entity"
	chatentity "health-nexus/internal/domain/chat/entity"
	configentity "health-nexus/internal/domain/config/entity"
	wikientity "health-nexus/internal/domain/wiki/entity"
)

// tableSpec 描述一张表的期望 schema，由 Go entity struct 反射推导。
type tableSpec struct {
	table      string          // DB 表名
	entityType any             // Go entity 零值（用于 reflect）
	viewFields map[string]bool // Go 字段名 → true 表示是 JOIN 生成的 View 字段，非 DB 列
	dbOnlyCols map[string]bool // DB 列名 → true 表示无对应 Go 字段（如触发器维护的列）
}

var specs = []tableSpec{
	{table: "departments", entityType: baseentity.Department{}},
	{
		table: "users", entityType: authentity.User{},
		viewFields: map[string]bool{"PrimaryDeptID": true},
	},
	{table: "articles", entityType: wikientity.Article{},
		viewFields: map[string]bool{"DepartmentName": true, "AuthorName": true},
	},
	{table: "article_chunks", entityType: wikientity.ArticleChunk{},
		dbOnlyCols: map[string]bool{"tsv": true},
	},
	{table: "article_references", entityType: wikientity.ArticleReference{},
		viewFields: map[string]bool{
			"ArticleTitle": true, "SourceDeptName": true,
			"TargetDeptName": true, "ApplicantName": true,
			"SourceArticleStatus": true,
		},
	},
	{table: "article_audit_logs", entityType: wikientity.ArticleAuditLog{}},
	{table: "conversations", entityType: chatentity.Conversation{}},
	{table: "messages", entityType: chatentity.Message{}},
	{table: "crisis_events", entityType: chatentity.CrisisEvent{}},
	{table: "ai_providers", entityType: configentity.AIProvider{}},
	{table: "sensitive_words", entityType: configentity.SensitiveWord{}},
	{table: "safety_rules", entityType: configentity.SafetyRule{}},
	{table: "rag_configs", entityType: configentity.RAGConfig{}},
	{table: "prompt_templates", entityType: configentity.PromptTemplate{}},
	{table: "safety_messages", entityType: configentity.SafetyMessage{}},
	{table: "config_audit_logs", entityType: configentity.ConfigAuditLog{}},
}

// userDepartmentsCols 是 user_departments 的期望列（无对应 Go entity，显式定义）。
var userDepartmentsCols = []string{"id", "user_id", "department_id", "is_primary", "created_at"}

// ── snake_case 转换 ─────────────────────────────────────────────────────────

// acronymOverride 处理连续大写字母（API/URL）的标准 snake_case 无法正确拆分的情况。
var acronymOverride = map[string]string{
	"APIURL":          "api_url",
	"IsFullURL":       "is_full_url",
	"APIKeyEncrypted": "api_key_encrypted",
	"APIKeyMasked":    "api_key_masked",
	"CoverImageURL":   "cover_image_url",
}

// toSnakeCase 将 Go 字段名转为 DB 列名（snake_case）。
// 大部分字段可自动转换，少数连续大写缩写通过 acronymOverride 显式映射。
func toSnakeCase(name string) string {
	if col, ok := acronymOverride[name]; ok {
		return col
	}
	var b strings.Builder
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'A' && c <= 'Z' {
			if i > 0 {
				prev := name[i-1]
				// lowercase → uppercase：插入下划线
				if prev >= 'a' && prev <= 'z' {
					b.WriteByte('_')
				} else if prev >= 'A' && prev <= 'Z' && i+1 < len(name) && name[i+1] >= 'a' && name[i+1] <= 'z' {
					// 连续大写序列末尾 + 后跟小写：在最后一个大写前插入下划线
					b.WriteByte('_')
				}
			}
			b.WriteByte(c + 32) // toLower
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// goColumns 通过反射获取 entity struct 的 DB 列名列表（排除 View 字段）。
func goColumns(entity any, viewFields map[string]bool) []string {
	t := reflect.TypeOf(entity)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	var cols []string
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if viewFields[field.Name] {
			continue
		}
		cols = append(cols, toSnakeCase(field.Name))
	}
	sort.Strings(cols)
	return cols
}

// ── DB 查询 ─────────────────────────────────────────────────────────────────

// dbColumns 查询 information_schema 获取表的实际列名列表。
func dbColumns(ctx context.Context, pool *pgxpool.Pool, table string) ([]string, error) {
	rows, err := pool.Query(ctx,
		`SELECT column_name FROM information_schema.columns
		 WHERE table_schema = 'public' AND table_name = $1
		 ORDER BY column_name`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		cols = append(cols, name)
	}
	return cols, rows.Err()
}

// ── 测试 ────────────────────────────────────────────────────────────────────

func mustPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("SCHEMA_TEST_DSN")
	if dsn == "" {
		dsn = "postgres://health:health@localhost:5432/health_nexus?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("DB 不可用，跳过 schema 校验: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("DB 不可达，跳过 schema 校验: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// TestSchemaMatchesEntities 校验每个 Go entity struct 的字段与 DB 表列一一对应。
// SQL 变了但 Go 没跟上（或反过来）→ 测试失败。
func TestSchemaMatchesEntities(t *testing.T) {
	pool := mustPool(t)
	ctx := context.Background()

	for _, spec := range specs {
		t.Run(spec.table, func(t *testing.T) {
			expected := goColumns(spec.entityType, spec.viewFields)
			actual, err := dbColumns(ctx, pool, spec.table)
			if err != nil {
				t.Fatalf("查询 information_schema: %v", err)
			}

			// 把 dbOnlyCols 加入 expected（这些列由触发器/系统维护，无对应 Go 字段）
			for col := range spec.dbOnlyCols {
				expected = append(expected, col)
			}
			sort.Strings(expected)

			// Go 有但 DB 没有
			goSet := setOf(expected)
			dbSet := setOf(actual)
			for col := range goSet {
				if !dbSet[col] {
					t.Errorf("Go 期望列 %q 在 DB 表 %s 中不存在（Go entity 未同步？）", col, spec.table)
				}
			}

			// DB 有但 Go 没有（可能是新加了列但忘记更新 entity）
			for col := range dbSet {
				if !goSet[col] {
					t.Errorf("DB 表 %s 列 %q 在 Go entity 中无对应字段（entity 未同步？）", spec.table, col)
				}
			}
		})
	}
}

// TestUserDepartmentsTable 校验 user_departments 表（无对应 Go entity，显式期望列）。
func TestUserDepartmentsTable(t *testing.T) {
	pool := mustPool(t)
	ctx := context.Background()

	actual, err := dbColumns(ctx, pool, "user_departments")
	if err != nil {
		t.Fatalf("查询 information_schema: %v", err)
	}

	expected := userDepartmentsCols
	sort.Strings(expected)

	goSet := setOf(expected)
	dbSet := setOf(actual)
	for _, col := range expected {
		if !dbSet[col] {
			t.Errorf("期望列 %q 在 user_departments 表中不存在", col)
		}
	}
	for col := range dbSet {
		if !goSet[col] {
			t.Errorf("DB 列 %q 未在期望列表中（迁移新增了列？）", col)
		}
	}
}

func setOf(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}
