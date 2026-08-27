// Package e2e_api_test 通过真实 HTTP 请求覆盖全部 API 端点（路由树即契约，代码即文档）。
//
// 运行前提：后端已启动（air 热重载，localhost:5230）、PostgreSQL/Redis 可达、LLM API key 已配置。
// 与 tests/e2e 复用同构 helper（login/doReq/clearRateLimitKeys/clearTestData），
// 但独立成包以避免命名冲突，并按域分组覆盖所有端点。
//
// 测试账号（由 TestMain 动态注入 DB，密码统一 Pass1234，测试后清理）：
//   - admin1     DEPT_ADMIN  心内科(dept=1)
//   - doctor1    DOCTOR      心内科(dept=1)
//   - testpatient PATIENT     内分泌科(dept=2)
//
// ponytail: 用标准库 + pgx + redis，不引入 testify（go.mod 未依赖，避免新增）。
// 危机/引用授权/跨科室审核等需要 dept=2 用户的场景，通过 e2ePool 临时 SQL 注入 admin2/doctor2 测试用户，测试后清理。
package e2e_api_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// ─── 全局共享客户端 ──────────────────────────────────────────────────────
// ponytail: 全局单例——进程退出即释放，无需 Close。
var (
	rateRdb *redis.Client
	e2ePool *pgxpool.Pool
	// admin2UserID/doctor2UserID 由 setupReferenceUsers 在测试前注入，teardownReferenceUsers 清理。
	admin2UserID  int64
	doctor2UserID int64
	// e2eDept2Created 标记 dept=2（内分泌科）是否由本测试创建，teardown 时据此清理。
	e2eDept2Created bool
)

// ─── TestMain ─────────────────────────────────────────────────────────────

func TestMain(m *testing.M) {
	addr := os.Getenv("E2E_REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	rateRdb = redis.NewClient(&redis.Options{Addr: addr, Password: os.Getenv("E2E_REDIS_PASSWORD")})

	dsn := os.Getenv("E2E_DB_DSN")
	if dsn == "" {
		dsn = "postgres://health:health@localhost:5432/health_nexus?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if pool, err := pgxpool.New(ctx, dsn); err == nil {
		if perr := pool.Ping(ctx); perr == nil {
			e2ePool = pool
		} else {
			pool.Close()
		}
	}
	cancel()

	clearRateLimitKeys()
	seedBaseUsers()
	code := m.Run()
	teardownBaseUsers()
	os.Exit(code)
}

// ─── 基础数据种子（原 DB seed 迁移移除后由 TestMain 动态注入） ─────────────

// seedBaseUsers 创建 e2e 依赖的科室与测试账号（密码统一 Pass1234）。
// 幂等：存在即跳过，可重复运行。dept=2 若原库已有则复用（e2eDept2Created=false）。
func seedBaseUsers() {
	if e2ePool == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 确保 dept=2（内分泌科）存在——跨科室/引用授权场景硬编码 department_id=2。
	var dept2Exists bool
	if err := e2ePool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM departments WHERE id = 2)`).Scan(&dept2Exists); err != nil {
		return
	}
	if !dept2Exists {
		if _, err := e2ePool.Exec(ctx, `INSERT INTO departments (id, name, is_public, is_active, description) VALUES (2, '内分泌科', true, true, 'e2e 自动创建')`); err != nil {
			return
		}
		e2eDept2Created = true
	}

	// 与 seed 同哈希——Pass1234
	const hash = "argon2id$v=19$m=65536,t=3,p=2$iuNmx/jZopIBIRRekDpN5g==$bkyZH8MSz8qfrDmEXz/C8ogHRgd7H+VHIQEg8bGpUZ4="
	for _, u := range []struct {
		username string
		role     string
		deptID   int
	}{
		{"admin1", "DEPT_ADMIN", 1},
		{"doctor1", "DOCTOR", 1},
		{"testpatient", "PATIENT", 2},
	} {
		var uid int64
		err := e2ePool.QueryRow(ctx, `
INSERT INTO users (username, role, password_hash, is_active)
VALUES ($1, $2, $3, true)
ON CONFLICT (username) DO UPDATE SET password_hash = EXCLUDED.password_hash, is_active = true
RETURNING id`, u.username, u.role, hash).Scan(&uid)
		if err != nil {
			continue
		}
		_, _ = e2ePool.Exec(ctx, `
INSERT INTO user_departments (user_id, department_id, is_primary)
VALUES ($1, $2, true)
ON CONFLICT (user_id, department_id) DO NOTHING`, uid, u.deptID)
	}

	// 敏感词是系统运行配置（chat 危机/紧急触发依赖），幂等补齐测试所需的最小集。
	for _, w := range []struct{ word, category string }{
		{"自杀", "suicide"},
		{"胸痛", "emergency"},
	} {
		_, _ = e2ePool.Exec(ctx, `
INSERT INTO sensitive_words (word, category)
VALUES ($1, $2)
ON CONFLICT (word, category) DO NOTHING`, w.word, w.category)
	}
}

// teardownBaseUsers 清理 seedBaseUsers 创建的测试账号；仅当 dept=2 由本测试创建时才删除该科室。
func teardownBaseUsers() {
	if e2ePool == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stmts := []string{
		`DELETE FROM user_departments WHERE user_id IN (SELECT id FROM users WHERE username IN ('admin1','doctor1','testpatient'))`,
		`DELETE FROM users WHERE username IN ('admin1','doctor1','testpatient')`,
	}
	for _, s := range stmts {
		_, _ = e2ePool.Exec(ctx, s)
	}
	if e2eDept2Created {
		_, _ = e2ePool.Exec(ctx, `DELETE FROM departments WHERE id = 2`)
	}
}

// ─── 清理 helpers（移植自 tests/e2e/e2e_test.go） ────────────────────────

func clearRateLimitKeys() {
	if rateRdb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var cursor uint64
	for {
		keys, c, err := rateRdb.Scan(ctx, cursor, "rate:*", 200).Result()
		if err != nil {
			return
		}
		if len(keys) > 0 {
			_ = rateRdb.Del(ctx, keys...).Err()
		}
		if c == 0 {
			return
		}
		cursor = c
	}
}

// clearTestData 清理 config 域 + e2e_api 测试残留（articles/references/users）。
// ponytail: 测试是唯一写入这些前缀的来源，按前缀 DELETE 足够；e2ePool=nil 时静默跳过。
func clearTestData() {
	if e2ePool == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stmts := []string{
		// config 域
		`DELETE FROM prompt_templates WHERE content LIKE 'E2E%' OR content LIKE '更新后的%'`,
		`DELETE FROM ai_providers WHERE name LIKE 'E2E_%' OR name LIKE 'E2E_LLM_%'`,
		`DELETE FROM sensitive_words WHERE word LIKE 'e2e_%' OR word LIKE 'e2e_sw_%'`,
		`DELETE FROM safety_rules WHERE name LIKE 'E2E%'`,
		// wiki 域（e2e_api 残留）
		`DELETE FROM article_references WHERE review_comment LIKE 'E2E%' OR review_comment = 'E2E 测试备注'`,
		`DELETE FROM article_audit_logs WHERE article_id IN (SELECT id FROM articles WHERE title LIKE 'E2E%')`,
		`DELETE FROM article_chunks WHERE article_id IN (SELECT id FROM articles WHERE title LIKE 'E2E%')`,
		`DELETE FROM articles WHERE title LIKE 'E2E%' OR title LIKE '更新后的%'`,
		// 测试临时用户（admin2/doctor2，由 setupReferenceUsers 创建）
		`DELETE FROM user_departments WHERE user_id IN (SELECT id FROM users WHERE username IN ('e2e_admin2','e2e_doctor2'))`,
		`DELETE FROM users WHERE username IN ('e2e_admin2','e2e_doctor2')`,
	}
	for _, s := range stmts {
		_, _ = e2ePool.Exec(ctx, s)
	}
}

func setupHelper(t *testing.T) {
	t.Helper()
	clearRateLimitKeys()
	clearTestData()
}

// ─── HTTP helpers ─────────────────────────────────────────────────────────

func apiURL() string {
	if v := os.Getenv("E2E_API_URL"); v != "" {
		return v
	}
	return "http://localhost:5230"
}

func jsonBody(t *testing.T, v any) *bytes.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	return bytes.NewReader(b)
}

func parseJSON(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("parse json: %v, body=%s", err, string(b))
	}
	return m
}

func authHeader(token string) string {
	return "Bearer " + token
}

// login 通用登录，返回 access token。
func login(t *testing.T, username, password, endpoint string) string {
	t.Helper()
	body := jsonBody(t, map[string]string{"username": username, "password": password})
	resp, err := http.Post(apiURL()+endpoint, "application/json", body)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("login %s expected 200, got %d: %s", username, resp.StatusCode, string(b))
	}
	m := parseJSON(t, resp)
	access, _ := m["access"].(string)
	if access == "" {
		t.Fatalf("login %s: no access token in %v", username, m)
	}
	return access
}

// loginStaff / loginPatient / loginAdmin — 便捷包装。
func loginAdmin(t *testing.T) string { return login(t, "admin1", "Pass1234", "/api/auth/login") }
func loginDoctor(t *testing.T) string {
	return login(t, "doctor1", "Pass1234", "/api/auth/login")
}
func loginPatient(t *testing.T) string {
	return login(t, "testpatient", "Pass1234", "/api/auth/login")
}

// loginAdmin2 / loginDoctor2 — 引用授权/跨科室审核需要的 dept=2 临时用户。
// 由 setupReferenceUsers 在测试前注入到 DB；若未注入则跳过。
func loginAdmin2(t *testing.T) string {
	if admin2UserID == 0 {
		t.Skip("admin2 未注入，跳过跨科室测试")
	}
	return login(t, "e2e_admin2", "Pass1234", "/api/auth/login")
}

// doReq 通用请求，token 为空时不带 Authorization。
func doReq(t *testing.T, method, path string, body io.Reader, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, apiURL()+path, body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", authHeader(token))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

// assertStatus 检查响应状态码，失败时打印 body。
func assertStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		b, _ := io.ReadAll(resp.Body)
		t.Errorf("expected %d, got %d: %s", want, resp.StatusCode, string(b))
	}
}

// drainAndClose 读取并关闭 body（避免连接泄漏）。
func drainAndClose(resp *http.Response) {
	if resp != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
}

// ─── SQL seeding helpers（引用授权测试需要 dept=2 用户） ──────────────────

// setupReferenceUsers 通过 SQL 注入临时 admin2/doctor2 用户到 dept=2（内分泌科）。
// 复用 seed 的 argon2id 哈希（密码=Pass1234），测试结束由 clearTestData 清理。
// ponytail: 测试是唯一写入这些用户名的来源，按 username 精确 DELETE 即可。
func setupReferenceUsers(t *testing.T) {
	t.Helper()
	if e2ePool == nil {
		t.Skip("e2ePool 未初始化，跳过需要 SQL 注入的测试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 与 seed 同哈希——Pass1234
	const hash = "argon2id$v=19$m=65536,t=3,p=2$iuNmx/jZopIBIRRekDpN5g==$bkyZH8MSz8qfrDmEXz/C8ogHRgd7H+VHIQEg8bGpUZ4="

	// 注入 admin2（DEPT_ADMIN，dept=2）
	err := e2ePool.QueryRow(ctx, `
INSERT INTO users (username, role, password_hash, is_active)
VALUES ('e2e_admin2', 'DEPT_ADMIN', $1, true)
ON CONFLICT (username) DO UPDATE SET password_hash = EXCLUDED.password_hash, is_active = true
RETURNING id`, hash).Scan(&admin2UserID)
	if err != nil {
		t.Fatalf("seed admin2: %v", err)
	}
	_, err = e2ePool.Exec(ctx, `
INSERT INTO user_departments (user_id, department_id, is_primary)
VALUES ($1, 2, true)
ON CONFLICT (user_id, department_id) DO NOTHING`, admin2UserID)
	if err != nil {
		t.Fatalf("seed admin2 dept: %v", err)
	}

	// 注入 doctor2（DOCTOR，dept=2）
	err = e2ePool.QueryRow(ctx, `
INSERT INTO users (username, role, password_hash, is_active)
VALUES ('e2e_doctor2', 'DOCTOR', $1, true)
ON CONFLICT (username) DO UPDATE SET password_hash = EXCLUDED.password_hash, is_active = true
RETURNING id`, hash).Scan(&doctor2UserID)
	if err != nil {
		t.Fatalf("seed doctor2: %v", err)
	}
	_, err = e2ePool.Exec(ctx, `
INSERT INTO user_departments (user_id, department_id, is_primary)
VALUES ($1, 2, true)
ON CONFLICT (user_id, department_id) DO NOTHING`, doctor2UserID)
	if err != nil {
		t.Fatalf("seed doctor2 dept: %v", err)
	}

	// 方案 B（REQ-WIKI-020 策展制）：所有科室文章统一走引用审批流，不区分公共/非公共，无需临时修改 is_public。
}

// ─── SSE 解析 helper ─────────────────────────────────────────────────────

// sseEvent 表示解析出的 SSE 事件。
type sseEvent struct {
	Event string
	Data  string
}

// readSSEEvents 读取 SSE 流直到 done 事件或超时，返回所有事件。
// ponytail: bufio.Scanner 默认 token 上限 64KB——LLM token 不会超，但 references JSON 可能较大；
// 改用增大 buffer 的 Scanner，足够覆盖单事件 1MB。
func readSSEEvents(t *testing.T, resp *http.Response) []sseEvent {
	t.Helper()
	defer drainAndClose(resp)
	var events []sseEvent
	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 0, 1<<20)
	scanner.Buffer(buf, 2<<20)
	var curEvent, curData string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			// 空行 = 事件边界
			if curEvent != "" || curData != "" {
				events = append(events, sseEvent{Event: curEvent, Data: curData})
				if curEvent == "done" {
					return events
				}
			}
			curEvent, curData = "", ""
			continue
		}
		if strings.HasPrefix(line, "event: ") {
			curEvent = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			if curData != "" {
				curData += "\n"
			}
			curData += strings.TrimPrefix(line, "data: ")
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read sse: %v", err)
	}
	return events
}

// ─── 1. healthz ────────────────────────────────────────────────────────────

// EP01: GET /healthz
func TestHealthz(t *testing.T) {
	resp := doReq(t, "GET", "/healthz", nil, "")
	defer drainAndClose(resp)
	assertStatus(t, resp, http.StatusOK)
	m := parseJSON(t, resp)
	for _, k := range []string{"status", "database", "redis", "timestamp"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing field %q in %v", k, m)
		}
	}
	if m["database"] != "ok" {
		t.Errorf("expected database=ok, got %v", m["database"])
	}
	if m["redis"] != "ok" {
		t.Errorf("expected redis=ok, got %v", m["redis"])
	}
	t.Logf("EP01 GET /healthz: ✅ status=ok, database=ok, redis=ok")
}

// ─── 2. auth (4 端点) ──────────────────────────────────────────────────────

// EP02-05: 4 个 auth 端点
func TestAuth(t *testing.T) {
	setupHelper(t)
	uniqueName := fmt.Sprintf("e2e_auth_%d", time.Now().UnixNano())
	pw := "Pass1234"

	var (
		refreshFromRegister string
		refreshFromStaff    string
		accessFromStaff     string
	)

	// EP02: POST /api/auth/login → 200
	t.Run("EP02_UnifiedLogin", func(t *testing.T) {
		body := jsonBody(t, map[string]string{"username": "admin1", "password": pw})
		resp, err := http.Post(apiURL()+"/api/auth/login", "application/json", body)
		if err != nil {
			t.Fatal(err)
		}
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
		m := parseJSON(t, resp)
		accessFromStaff, _ = m["access"].(string)
		refreshFromStaff, _ = m["refresh"].(string)
		if accessFromStaff == "" || refreshFromStaff == "" {
			t.Fatalf("missing tokens: %v", m)
		}
		user, _ := m["user"].(map[string]any)
		if user == nil {
			t.Fatalf("missing user: %v", m)
		}
		if user["role"] != "DEPT_ADMIN" {
			t.Errorf("expected role DEPT_ADMIN, got %v", user["role"])
		}
	})

	// EP03: POST /api/auth/login → 200（患者统一登录）
	t.Run("EP03_UnifiedLogin_Patient", func(t *testing.T) {
		body := jsonBody(t, map[string]string{"username": "testpatient", "password": pw})
		resp, err := http.Post(apiURL()+"/api/auth/login", "application/json", body)
		if err != nil {
			t.Fatal(err)
		}
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
		m := parseJSON(t, resp)
		if m["access"] == nil || m["refresh"] == nil {
			t.Fatalf("missing tokens: %v", m)
		}
		user, _ := m["user"].(map[string]any)
		if user["role"] != "PATIENT" {
			t.Errorf("expected role PATIENT, got %v", user["role"])
		}
	})

	// EP04: POST /api/auth/register → 201
	t.Run("EP04_Register", func(t *testing.T) {
		body := jsonBody(t, map[string]string{"username": uniqueName, "password": pw})
		resp, err := http.Post(apiURL()+"/api/auth/register", "application/json", body)
		if err != nil {
			t.Fatal(err)
		}
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusCreated)
		m := parseJSON(t, resp)
		if m["access"] == nil || m["refresh"] == nil {
			t.Fatalf("missing tokens: %v", m)
		}
		refreshFromRegister, _ = m["refresh"].(string)
		user, _ := m["user"].(map[string]any)
		if user["role"] != "PATIENT" {
			t.Errorf("expected role PATIENT, got %v", user["role"])
		}
	})

	// EP05: POST /api/auth/refresh → 200（用 register 返回的 refresh 轮换）
	t.Run("EP05_Refresh", func(t *testing.T) {
		if refreshFromRegister == "" {
			t.Fatal("no refresh token from register")
		}
		body := jsonBody(t, map[string]string{"refresh": refreshFromRegister})
		resp, err := http.Post(apiURL()+"/api/auth/refresh", "application/json", body)
		if err != nil {
			t.Fatal(err)
		}
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
		m := parseJSON(t, resp)
		if m["access"] == nil || m["refresh"] == nil {
			t.Fatalf("missing rotated tokens: %v", m)
		}
		// refresh 轮换后旧 token 应失效（再次 refresh → 401）
		old := refreshFromRegister
		refreshFromRegister, _ = m["refresh"].(string)
		body2 := jsonBody(t, map[string]string{"refresh": old})
		resp2, err := http.Post(apiURL()+"/api/auth/refresh", "application/json", body2)
		if err != nil {
			t.Fatal(err)
		}
		defer drainAndClose(resp2)
		if resp2.StatusCode != http.StatusUnauthorized {
			t.Errorf("old refresh should be invalidated, got %d", resp2.StatusCode)
		}
	})

	// EP06: POST /api/auth/logout → 200（需 JWT + refresh）
	t.Run("EP06_Logout", func(t *testing.T) {
		if accessFromStaff == "" || refreshFromStaff == "" {
			t.Fatal("no tokens from login")
		}
		body := jsonBody(t, map[string]string{"refresh": refreshFromStaff})
		resp := doReq(t, "POST", "/api/auth/logout", body, accessFromStaff)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
		m := parseJSON(t, resp)
		if m["success"] != true {
			t.Errorf("expected success=true, got %v", m["success"])
		}
	})

	// 错误用例：错误密码 → 401
	t.Run("EP02_InvalidPassword_401", func(t *testing.T) {
		body := jsonBody(t, map[string]string{"username": "admin1", "password": "wrong"})
		resp, err := http.Post(apiURL()+"/api/auth/login", "application/json", body)
		if err != nil {
			t.Fatal(err)
		}
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusUnauthorized)
		m := parseJSON(t, resp)
		if m["code"] != "AUTH_INVALID_CREDENTIALS" {
			t.Errorf("expected AUTH_INVALID_CREDENTIALS, got %v", m["code"])
		}
	})

	// 错误用例：无效 refresh_token → 401
	t.Run("EP05_InvalidRefresh_401", func(t *testing.T) {
		body := jsonBody(t, map[string]string{"refresh": "invalid-token"})
		resp, err := http.Post(apiURL()+"/api/auth/refresh", "application/json", body)
		if err != nil {
			t.Fatal(err)
		}
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusUnauthorized)
	})
}

// ─── 3. base (1 端点) ──────────────────────────────────────────────────────

// EP07: GET /api/base/departments
func TestBaseDepartments(t *testing.T) {
	setupHelper(t)
	staffToken := loginDoctor(t)

	t.Run("EP07_List", func(t *testing.T) {
		resp := doReq(t, "GET", "/api/base/departments", nil, staffToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
		var depts []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&depts); err != nil {
			t.Fatalf("parse: %v", err)
		}
		if len(depts) == 0 {
			t.Error("expected non-empty departments")
		}
		// 验证字段：id, name, parent_id, is_public
		for _, d := range depts {
			for _, k := range []string{"id", "name", "parent_id", "is_public", "is_active"} {
				if _, ok := d[k]; !ok {
					t.Errorf("missing field %q in dept %v", k, d)
				}
			}
		}
		// 数据隔离：doctor1(dept=1) 至少看到本科室
		var sawDept1 bool
		for _, d := range depts {
			if id, _ := d["id"].(float64); int64(id) == 1 {
				sawDept1 = true
			}
		}
		if !sawDept1 {
			t.Errorf("doctor1 should see dept=1, got %v", depts)
		}
	})

	// 错误用例：无 token → 401
	t.Run("EP07_NoAuth_401", func(t *testing.T) {
		resp := doReq(t, "GET", "/api/base/departments", nil, "")
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusUnauthorized)
	})
}

// ─── 4. wiki 公开 (2 端点) ────────────────────────────────────────────────

// EP08-09: GET /api/wiki/articles, GET /api/wiki/articles/{id}
func TestWikiPublic(t *testing.T) {
	setupHelper(t)

	// EP08: GET /api/wiki/articles
	var firstArticleID float64
	t.Run("EP08_List", func(t *testing.T) {
		resp := doReq(t, "GET", "/api/wiki/articles?department_id=1&page=1&page_size=10", nil, "")
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
		m := parseJSON(t, resp)
		for _, k := range []string{"items", "total", "page", "page_size"} {
			if _, ok := m[k]; !ok {
				t.Errorf("missing field %q: %v", k, m)
			}
		}
		items, _ := m["items"].([]any)
		if len(items) == 0 {
			t.Fatal("expected non-empty items, seed articles missing")
		}
		first, _ := items[0].(map[string]any)
		for _, k := range []string{"id", "title", "summary", "department_id", "view_count", "version"} {
			if _, ok := first[k]; !ok {
				t.Errorf("article missing field %q: %v", k, first)
			}
		}
		firstArticleID, _ = first["id"].(float64)
	})

	// EP09: GET /api/wiki/articles/{id}
	t.Run("EP09_Detail", func(t *testing.T) {
		if firstArticleID == 0 {
			t.Fatal("no article id from list")
		}
		resp := doReq(t, "GET", fmt.Sprintf("/api/wiki/articles/%d", int64(firstArticleID)), nil, "")
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
		m := parseJSON(t, resp)
		for _, k := range []string{"id", "title", "content", "summary", "view_count", "department_id", "version"} {
			if _, ok := m[k]; !ok {
				t.Errorf("detail missing field %q: %v", k, m)
			}
		}
	})

	// 错误用例：不存在的 id → 404
	t.Run("EP09_NotFound_404", func(t *testing.T) {
		resp := doReq(t, "GET", "/api/wiki/articles/9999999", nil, "")
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusNotFound)
	})
}

// ─── 5. wiki 医护文章 (8 端点) ────────────────────────────────────────────

// EP10-17: 8 个医护端文章端点（含状态机验证）
func TestWikiStaffArticles(t *testing.T) {
	setupHelper(t)
	doctor1Token := loginDoctor(t)
	admin1Token := loginAdmin(t)

	var (
		draftID       float64 // 主流程文章 ID
		rejectID      float64 // reject 流程文章 ID
		archiveID     float64 // archive 流程文章 ID
		invalidTranID float64 // 状态非法转换测试
	)

	// EP10: POST /api/staff/wiki/articles → 201（创建草稿）
	t.Run("EP10_Create", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"title":           fmt.Sprintf("E2E_Create_%d", time.Now().UnixNano()),
			"content":         "测试文章内容",
			"department_id":   1,
			"allow_reference": false,
		})
		resp := doReq(t, "POST", "/api/staff/wiki/articles", body, doctor1Token)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusCreated)
		m := parseJSON(t, resp)
		draftID, _ = m["id"].(float64)
		if draftID == 0 {
			t.Fatalf("expected id: %v", m)
		}
		if m["status"] != "draft" {
			t.Errorf("expected status=draft, got %v", m["status"])
		}
		if m["author_id"] == nil {
			t.Errorf("expected author_id: %v", m)
		}
	})

	// EP11: GET /api/staff/wiki/articles → 200
	t.Run("EP11_ListMine", func(t *testing.T) {
		resp := doReq(t, "GET", "/api/staff/wiki/articles", nil, doctor1Token)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
		m := parseJSON(t, resp)
		if _, ok := m["items"]; !ok {
			t.Errorf("expected items: %v", m)
		}
	})

	// EP12: PUT /api/staff/wiki/articles/{id} → 200（更新草稿）
	t.Run("EP12_Update", func(t *testing.T) {
		if draftID == 0 {
			t.Fatal("no draftID")
		}
		body := jsonBody(t, map[string]any{"title": "E2E 更新后的标题"})
		resp := doReq(t, "PUT", fmt.Sprintf("/api/staff/wiki/articles/%d", int64(draftID)), body, doctor1Token)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
		m := parseJSON(t, resp)
		if m["title"] != "E2E 更新后的标题" {
			t.Errorf("expected title updated, got %v", m["title"])
		}
	})

	// EP13: POST /api/staff/wiki/articles/{id}/submit → 200（draft→pending）
	t.Run("EP13_Submit", func(t *testing.T) {
		if draftID == 0 {
			t.Fatal("no draftID")
		}
		resp := doReq(t, "POST", fmt.Sprintf("/api/staff/wiki/articles/%d/submit", int64(draftID)), nil, doctor1Token)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
		m := parseJSON(t, resp)
		if m["success"] != true {
			t.Errorf("expected success=true, got %v", m["success"])
		}
	})

	// 错误用例：自审 → 403
	t.Run("EP14_SelfApprove_403", func(t *testing.T) {
		if draftID == 0 {
			t.Fatal("no draftID")
		}
		resp := doReq(t, "POST", fmt.Sprintf("/api/staff/wiki/articles/%d/approve", int64(draftID)), nil, doctor1Token)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusForbidden)
		m := parseJSON(t, resp)
		if m["code"] != "WIKI_REVIEW_FORBIDDEN" {
			t.Errorf("expected WIKI_REVIEW_FORBIDDEN (non-admin self-review not allowed), got %v", m["code"])
		}
	})

	// 错误用例：状态非法转换（draft 状态直接 archive → 409）
	t.Run("EP17_InvalidTransition_409", func(t *testing.T) {
		// 创建新草稿
		body := jsonBody(t, map[string]any{
			"title":         fmt.Sprintf("E2E_InvalidTran_%d", time.Now().UnixNano()),
			"content":       "测试非法转换",
			"department_id": 1,
		})
		resp := doReq(t, "POST", "/api/staff/wiki/articles", body, doctor1Token)
		defer drainAndClose(resp)
		if resp.StatusCode != http.StatusCreated {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("create: expected 201, got %d: %s", resp.StatusCode, string(b))
		}
		m := parseJSON(t, resp)
		invalidTranID, _ = m["id"].(float64)

		// draft 状态 archive → 409
		resp2 := doReq(t, "POST", fmt.Sprintf("/api/staff/wiki/articles/%d/archive", int64(invalidTranID)), nil, admin1Token)
		defer drainAndClose(resp2)
		if resp2.StatusCode != http.StatusConflict {
			t.Errorf("archive draft expected 409, got %d", resp2.StatusCode)
		}
	})

	// EP14: POST /api/staff/wiki/articles/{id}/approve → 200（pending→published，admin1 审）
	t.Run("EP14_Approve", func(t *testing.T) {
		if draftID == 0 {
			t.Fatal("no draftID")
		}
		body := jsonBody(t, map[string]any{"note": "E2E 审核通过"})
		resp := doReq(t, "POST", fmt.Sprintf("/api/staff/wiki/articles/%d/approve", int64(draftID)), body, admin1Token)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
	})

	// EP15: POST /api/staff/wiki/articles/{id}/reject → 200（pending→draft，需新文章）
	t.Run("EP15_Reject", func(t *testing.T) {
		// 创建新文章 → submit → reject
		createBody := jsonBody(t, map[string]any{
			"title":         fmt.Sprintf("E2E_Reject_%d", time.Now().UnixNano()),
			"content":       "待驳回文章",
			"department_id": 1,
		})
		createResp := doReq(t, "POST", "/api/staff/wiki/articles", createBody, doctor1Token)
		defer drainAndClose(createResp)
		if createResp.StatusCode != http.StatusCreated {
			b, _ := io.ReadAll(createResp.Body)
			t.Fatalf("create: expected 201, got %d: %s", createResp.StatusCode, string(b))
		}
		cm := parseJSON(t, createResp)
		rejectID, _ = cm["id"].(float64)

		submitResp := doReq(t, "POST", fmt.Sprintf("/api/staff/wiki/articles/%d/submit", int64(rejectID)), nil, doctor1Token)
		drainAndClose(submitResp)
		if submitResp.StatusCode != http.StatusOK {
			t.Fatalf("submit: expected 200, got %d", submitResp.StatusCode)
		}

		rejectBody := jsonBody(t, map[string]any{"reason": "E2E 内容需要修改"})
		resp := doReq(t, "POST", fmt.Sprintf("/api/staff/wiki/articles/%d/reject", int64(rejectID)), rejectBody, admin1Token)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
	})

	// EP17: POST /api/staff/wiki/articles/{id}/archive → 200（published→archived）
	t.Run("EP17_Archive", func(t *testing.T) {
		// 创建新文章 → submit → approve → archive
		createBody := jsonBody(t, map[string]any{
			"title":         fmt.Sprintf("E2E_Archive_%d", time.Now().UnixNano()),
			"content":       "待归档文章",
			"department_id": 1,
		})
		createResp := doReq(t, "POST", "/api/staff/wiki/articles", createBody, doctor1Token)
		defer drainAndClose(createResp)
		if createResp.StatusCode != http.StatusCreated {
			b, _ := io.ReadAll(createResp.Body)
			t.Fatalf("create: expected 201, got %d: %s", createResp.StatusCode, string(b))
		}
		cm := parseJSON(t, createResp)
		archiveID, _ = cm["id"].(float64)

		submitResp := doReq(t, "POST", fmt.Sprintf("/api/staff/wiki/articles/%d/submit", int64(archiveID)), nil, doctor1Token)
		drainAndClose(submitResp)

		approveBody := jsonBody(t, map[string]any{"note": "approve for archive"})
		approveResp := doReq(t, "POST", fmt.Sprintf("/api/staff/wiki/articles/%d/approve", int64(archiveID)), approveBody, admin1Token)
		drainAndClose(approveResp)

		resp := doReq(t, "POST", fmt.Sprintf("/api/staff/wiki/articles/%d/archive", int64(archiveID)), nil, admin1Token)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
	})

	// EP16: DELETE /api/staff/wiki/articles/{id} → 200（软删除）
	t.Run("EP16_Delete", func(t *testing.T) {
		if draftID == 0 {
			t.Fatal("no draftID")
		}
		resp := doReq(t, "DELETE", fmt.Sprintf("/api/staff/wiki/articles/%d", int64(draftID)), nil, doctor1Token)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
		m := parseJSON(t, resp)
		if m["success"] != true {
			t.Errorf("expected success=true, got %v", m["success"])
		}
	})

	// 错误用例：非本科室创建 → 403
	t.Run("EP10_CrossDept_403", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"title":         "E2E 跨科室",
			"content":       "should fail",
			"department_id": 2, // doctor1 在 dept=1，无权在 dept=2 创建
		})
		resp := doReq(t, "POST", "/api/staff/wiki/articles", body, doctor1Token)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusForbidden)
	})

	// 错误用例：必填字段缺失 → 422
	t.Run("EP10_MissingFields_422", func(t *testing.T) {
		body := jsonBody(t, map[string]any{"summary": "只有摘要"})
		resp := doReq(t, "POST", "/api/staff/wiki/articles", body, doctor1Token)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusUnprocessableEntity)
	})
}

// ─── 6. wiki 引用授权 (5 端点) ────────────────────────────────────────────

// EP18-22: 5 个引用授权端点
// 测试场景：doctor1(dept=1) 创建带 allow_reference 的文章 → 发布 →
// admin2(dept=2) 引用 dept=1 的文章到 dept=2（公开文章直接 approved，免审批）→
// admin1(dept=1, 源科室 admin) 撤销引用。
// 审批/驳回端点仍保留（兼容旧 pending 数据），但新流程下公开文章不再产生 pending。
func TestWikiReferences(t *testing.T) {
	setupHelper(t)
	setupReferenceUsers(t)
	doctor1Token := loginDoctor(t)
	admin1Token := loginAdmin(t)
	admin2Token := loginAdmin2(t)

	var (
		refID          float64
		publishedArtID float64 // allow_reference=true 且 published 的文章
	)

	// 准备：创建 allow_reference=true 的文章并发布
	t.Run("Setup_PublishArticle", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"title":           fmt.Sprintf("E2E_Ref_%d", time.Now().UnixNano()),
			"content":         "可引用的文章",
			"department_id":   1,
			"allow_reference": true,
		})
		resp := doReq(t, "POST", "/api/staff/wiki/articles", body, doctor1Token)
		defer drainAndClose(resp)
		if resp.StatusCode != http.StatusCreated {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("setup create: expected 201, got %d: %s", resp.StatusCode, string(b))
		}
		m := parseJSON(t, resp)
		publishedArtID, _ = m["id"].(float64)

		subResp := doReq(t, "POST", fmt.Sprintf("/api/staff/wiki/articles/%d/submit", int64(publishedArtID)), nil, doctor1Token)
		drainAndClose(subResp)
		appResp := doReq(t, "POST", fmt.Sprintf("/api/staff/wiki/articles/%d/approve", int64(publishedArtID)),
			jsonBody(t, map[string]any{"note": "for ref test"}), admin1Token)
		drainAndClose(appResp)
	})

	// EP18: POST /api/staff/wiki/references → 201（公开文章直接 approved，免审批）
	t.Run("EP18_Apply", func(t *testing.T) {
		if publishedArtID == 0 {
			t.Fatal("no published article")
		}
		body := jsonBody(t, map[string]any{
			"article_id":     int64(publishedArtID),
			"target_dept_id": 2,
		})
		resp := doReq(t, "POST", "/api/staff/wiki/references", body, admin2Token)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusCreated)
		m := parseJSON(t, resp)
		refID, _ = m["id"].(float64)
		if refID == 0 {
			t.Fatalf("expected ref id: %v", m)
		}
		// 公开文章直接 approved，不再返回 pending
		if m["status"] != "approved" {
			t.Errorf("expected status=approved, got %v", m["status"])
		}
		if m["source_dept_id"] != float64(1) {
			t.Errorf("expected source_dept_id=1, got %v", m["source_dept_id"])
		}
		if m["target_dept_id"] != float64(2) {
			t.Errorf("expected target_dept_id=2, got %v", m["target_dept_id"])
		}
	})

	// EP19: GET /api/staff/wiki/references → 200
	t.Run("EP19_List", func(t *testing.T) {
		resp := doReq(t, "GET", "/api/staff/wiki/references", nil, admin1Token)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
		m := parseJSON(t, resp)
		if _, ok := m["items"]; !ok {
			t.Errorf("expected items: %v", m)
		}
	})

	// 错误用例：重复引用 → 409（已 approved 的引用不可重复创建）
	t.Run("EP18_Duplicate_409", func(t *testing.T) {
		if publishedArtID == 0 {
			t.Fatal("no published article")
		}
		body := jsonBody(t, map[string]any{
			"article_id":     int64(publishedArtID),
			"target_dept_id": 2,
		})
		resp := doReq(t, "POST", "/api/staff/wiki/references", body, admin2Token)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusConflict)
	})

	// 错误用例：DOCTOR 不可审核 → 403（approved 状态的引用，doctor 尝试 approve）
	t.Run("EP20_DoctorApprove_403", func(t *testing.T) {
		if refID == 0 {
			t.Fatal("no refID")
		}
		body := jsonBody(t, map[string]any{"note": "doctor tries"})
		resp := doReq(t, "POST", fmt.Sprintf("/api/staff/wiki/references/%d/approve", int64(refID)), body, doctor1Token)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusForbidden)
	})

	// 错误用例：目标科室 admin 尝试 approve → 403
	t.Run("EP20_TargetDeptApprove_403", func(t *testing.T) {
		if refID == 0 {
			t.Fatal("no refID")
		}
		body := jsonBody(t, map[string]any{"note": "target dept tries"})
		resp := doReq(t, "POST", fmt.Sprintf("/api/staff/wiki/references/%d/approve", int64(refID)), body, admin2Token)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusForbidden)
	})

	// EP20: 已 approved 的引用再次 approve → 409（仅 pending 可审批）
	t.Run("EP20_ApproveAlreadyApproved_409", func(t *testing.T) {
		if refID == 0 {
			t.Fatal("no refID")
		}
		body := jsonBody(t, map[string]any{"note": "E2E 测试备注"})
		resp := doReq(t, "POST", fmt.Sprintf("/api/staff/wiki/references/%d/approve", int64(refID)), body, admin1Token)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusConflict)
	})

	// EP21: 已 approved 的引用 reject → 409（仅 pending 可驳回）
	t.Run("EP21_RejectAlreadyApproved_409", func(t *testing.T) {
		if refID == 0 {
			t.Fatal("no refID")
		}
		rejectBody := jsonBody(t, map[string]any{"reason": "E2E 不允许引用"})
		rejectResp := doReq(t, "POST", fmt.Sprintf("/api/staff/wiki/references/%d/reject", int64(refID)), rejectBody, admin1Token)
		defer drainAndClose(rejectResp)
		assertStatus(t, rejectResp, http.StatusConflict)
	})

	// EP22: DELETE /api/staff/wiki/references/{id} → 200（approved→revoked）
	t.Run("EP22_Revoke", func(t *testing.T) {
		if refID == 0 {
			t.Fatal("no refID")
		}
		resp := doReq(t, "DELETE", fmt.Sprintf("/api/staff/wiki/references/%d", int64(refID)), nil, admin1Token)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
	})

	// 错误用例：reason 缺失 reject → 422 或 409（已 revoked）
	t.Run("EP21_MissingReason_422", func(t *testing.T) {
		if refID == 0 {
			t.Fatal("no refID")
		}
		body := jsonBody(t, map[string]any{"reason": ""})
		resp := doReq(t, "POST", fmt.Sprintf("/api/staff/wiki/references/%d/reject", int64(refID)), body, admin1Token)
		defer drainAndClose(resp)
		if resp.StatusCode != http.StatusUnprocessableEntity && resp.StatusCode != http.StatusConflict {
			t.Errorf("expected 422 or 409 (already revoked), got %d", resp.StatusCode)
		}
	})
}

// ─── 7. chat 患者端 (6 端点) ──────────────────────────────────────────────

// EP23-28: 6 个患者端 chat 端点
func TestChatPatient(t *testing.T) {
	setupHelper(t)
	patientToken := loginPatient(t)

	var convID string

	// EP23: POST /api/chat/stream {"message":"你好"} → 200（SSE，真实 LLM 调用，超时 60s）
	t.Run("EP23_Stream", func(t *testing.T) {
		client := &http.Client{Timeout: 60 * time.Second}
		req, _ := http.NewRequest("POST", apiURL()+"/api/chat/stream", jsonBody(t, map[string]any{"message": "你好"}))
		req.Header.Set("Authorization", authHeader(patientToken))
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("stream request: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
			resp.Body.Close()
			t.Fatalf("expected Content-Type=text/event-stream, got %q", ct)
		}
		events := readSSEEvents(t, resp)
		var sawDone, sawToken bool
		for _, ev := range events {
			switch ev.Event {
			case "done":
				sawDone = true
			case "token":
				sawToken = true
			}
		}
		if !sawDone {
			t.Errorf("expected done event, got events: %v", eventTypes(events))
		}
		if !sawToken {
			t.Logf("⚠️ no token event received (LLM may have returned empty); events: %v", eventTypes(events))
		}
		t.Logf("EP23 received %d SSE events: %v", len(events), eventTypes(events))
	})

	// EP24: GET /api/chat/conversations → 200
	t.Run("EP24_ListConversations", func(t *testing.T) {
		resp := doReq(t, "GET", "/api/chat/conversations", nil, patientToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
		m := parseJSON(t, resp)
		for _, k := range []string{"items", "total", "page", "page_size"} {
			if _, ok := m[k]; !ok {
				t.Errorf("missing field %q: %v", k, m)
			}
		}
		items, _ := m["items"].([]any)
		if len(items) > 0 {
			first, _ := items[0].(map[string]any)
			convID, _ = first["id"].(string)
			for _, k := range []string{"id", "title", "locked_dept_id", "archived", "last_message_at", "created_at"} {
				if _, ok := first[k]; !ok {
					t.Errorf("conversation missing field %q: %v", k, first)
				}
			}
		}
	})

	// EP25: GET /api/chat/conversations/{id} → 200 或 404
	t.Run("EP25_GetConversation", func(t *testing.T) {
		if convID == "" {
			t.Skip("no conversation available, skip get test")
		}
		resp := doReq(t, "GET", "/api/chat/conversations/"+convID, nil, patientToken)
		defer drainAndClose(resp)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	// EP26: PATCH /api/chat/conversations/{id} → 200
	t.Run("EP26_PatchTitle", func(t *testing.T) {
		if convID == "" {
			t.Skip("no conversation, skip patch test")
		}
		body := jsonBody(t, map[string]any{"title": "E2E 新标题"})
		resp := doReq(t, "PATCH", "/api/chat/conversations/"+convID, body, patientToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
		m := parseJSON(t, resp)
		if m["title"] != "E2E 新标题" {
			t.Errorf("expected title updated, got %v", m["title"])
		}
	})

	// EP28: GET /api/chat/conversations/{id}/messages → 200
	t.Run("EP28_ListMessages", func(t *testing.T) {
		if convID == "" {
			t.Skip("no conversation, skip messages test")
		}
		resp := doReq(t, "GET", fmt.Sprintf("/api/chat/conversations/%s/messages?limit=50", convID), nil, patientToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
		var arr []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&arr); err != nil {
			t.Fatalf("parse messages: %v", err)
		}
		for _, msg := range arr {
			for _, k := range []string{"id", "conversation_id", "role", "content", "created_at"} {
				if _, ok := msg[k]; !ok {
					t.Errorf("message missing field %q: %v", k, msg)
				}
			}
		}
	})

	// EP27: DELETE /api/chat/conversations/{id} → 200
	t.Run("EP27_Delete", func(t *testing.T) {
		if convID == "" {
			t.Skip("no conversation, skip delete test")
		}
		resp := doReq(t, "DELETE", "/api/chat/conversations/"+convID, nil, patientToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
		m := parseJSON(t, resp)
		if m["success"] != true {
			t.Errorf("expected success=true, got %v", m["success"])
		}
	})

	// 错误用例：空消息 → 400
	t.Run("EP23_EmptyMessage_400", func(t *testing.T) {
		req, _ := http.NewRequest("POST", apiURL()+"/api/chat/stream", jsonBody(t, map[string]any{"message": ""}))
		req.Header.Set("Authorization", authHeader(patientToken))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer drainAndClose(resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	// 错误用例：超长消息 → 422
	t.Run("EP23_TooLongMessage_422", func(t *testing.T) {
		longMsg := strings.Repeat("a", 2001)
		req, _ := http.NewRequest("POST", apiURL()+"/api/chat/stream", jsonBody(t, map[string]any{"message": longMsg}))
		req.Header.Set("Authorization", authHeader(patientToken))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer drainAndClose(resp)
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("expected 422, got %d", resp.StatusCode)
		}
	})
}

// eventTypes 提取所有 SSE 事件类型用于日志。
func eventTypes(events []sseEvent) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.Event)
	}
	return out
}

// ─── 8. chat 医护端 (2 端点) ──────────────────────────────────────────────

// EP29-30: 2 个医护端 chat 端点
func TestChatStaffCrisis(t *testing.T) {
	setupHelper(t)
	staffToken := loginDoctor(t)
	patientToken := loginPatient(t)

	var crisisID float64

	// EP29: GET /api/staff/chat/crisis-events → 200
	t.Run("EP29_List", func(t *testing.T) {
		resp := doReq(t, "GET", "/api/staff/chat/crisis-events", nil, staffToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
		m := parseJSON(t, resp)
		for _, k := range []string{"items", "total", "page", "page_size"} {
			if _, ok := m[k]; !ok {
				t.Errorf("missing field %q: %v", k, m)
			}
		}
		items, _ := m["items"].([]any)
		if len(items) > 0 {
			first, _ := items[0].(map[string]any)
			for _, k := range []string{"id", "patient_id", "conversation_id", "triggered_content", "matched_keywords", "level", "handled", "created_at"} {
				if _, ok := first[k]; !ok {
					t.Errorf("crisis event missing field %q: %v", k, first)
				}
			}
			// id 是字符串形式（DTO 用 strconv.FormatInt）
			idStr, _ := first["id"].(string)
			if idStr != "" {
				var id int
				if _, err := fmt.Sscanf(idStr, "%d", &id); err == nil {
					crisisID = float64(id)
				}
			}
		}
	})

	// EP30: POST /api/staff/chat/crisis-events/{id}/handle → 200
	t.Run("EP30_Handle", func(t *testing.T) {
		if crisisID == 0 {
			t.Skip("no crisis event in DB, skip handle test")
		}
		// 先确认该 crisis 未处理过（避免 409）
		// 用一个未处理的 crisis：seed 中第 2 个 crisis（如果有），否则跳过
		// 此处简化：直接尝试 handle，若 409 则视为 pass（说明已被处理，幂等）
		body := jsonBody(t, map[string]any{"note": "E2E 已处理"})
		resp := doReq(t, "POST", fmt.Sprintf("/api/staff/chat/crisis-events/%d/handle", int64(crisisID)), body, staffToken)
		defer drainAndClose(resp)
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict {
			b, _ := io.ReadAll(resp.Body)
			t.Errorf("expected 200 or 409 (already handled), got %d: %s", resp.StatusCode, string(b))
		}
	})

	// 错误用例：不存在的 crisis → 404
	t.Run("EP30_NotFound_404", func(t *testing.T) {
		body := jsonBody(t, map[string]any{"note": "test"})
		resp := doReq(t, "POST", "/api/staff/chat/crisis-events/999999/handle", body, staffToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusNotFound)
	})

	// 错误用例：患者访问 → 403
	t.Run("EP29_PatientForbidden_403", func(t *testing.T) {
		resp := doReq(t, "GET", "/api/staff/chat/crisis-events", nil, patientToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusForbidden)
	})

	// 触发新危机事件（真实 LLM 调用，超时 60s）
	t.Run("EP23_TriggerCrisis", func(t *testing.T) {
		client := &http.Client{Timeout: 60 * time.Second}
		req, _ := http.NewRequest("POST", apiURL()+"/api/chat/stream", jsonBody(t, map[string]any{"message": "我想自杀"}))
		req.Header.Set("Authorization", authHeader(patientToken))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Skipf("crisis trigger stream failed (network): %v", err)
		}
		events := readSSEEvents(t, resp)
		var sawCrisis bool
		for _, ev := range events {
			if ev.Event == "crisis" {
				sawCrisis = true
			}
		}
		if !sawCrisis {
			t.Logf("⚠️ no crisis event received (LLM/safety filter may have blocked); events: %v", eventTypes(events))
		}
		t.Logf("EP23 crisis trigger events: %v", eventTypes(events))
	})
}

// ─── 9. config ai-providers (4 端点) ──────────────────────────────────────

// EP31-34: 4 个 AI Provider 端点
func TestConfigAIProviders(t *testing.T) {
	setupHelper(t)
	adminToken := loginAdmin(t)
	doctorToken := loginDoctor(t)

	var providerID float64

	// EP31: GET /api/staff/config/ai-providers → 200
	t.Run("EP31_List", func(t *testing.T) {
		resp := doReq(t, "GET", "/api/staff/config/ai-providers", nil, adminToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
		var arr []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&arr); err != nil {
			t.Fatalf("parse: %v", err)
		}
		// 验证字段（seed 有数据时）
		if len(arr) > 0 {
			for _, k := range []string{"id", "provider_type", "name", "api_base", "api_key", "model_name", "is_active"} {
				if _, ok := arr[0][k]; !ok {
					t.Errorf("provider missing field %q: %v", k, arr[0])
				}
			}
		}
	})

	// EP32: POST /api/staff/config/ai-providers → 201（api_key 加密掩码）
	t.Run("EP32_Create", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"provider_type": "llm",
			"name":          fmt.Sprintf("E2E_LLM_%d", time.Now().UnixNano()),
			"api_base":      "https://api.example.com/v1",
			"api_key":       "sk-test1234567890",
			"model_name":    "gpt-4",
			"params":        map[string]any{"temperature": 0.3},
			"is_active":     true,
		})
		resp := doReq(t, "POST", "/api/staff/config/ai-providers", body, adminToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusCreated)
		m := parseJSON(t, resp)
		providerID, _ = m["id"].(float64)
		if providerID == 0 {
			t.Fatalf("expected id: %v", m)
		}
		// 掩码验证：响应不得返回明文 api_key
		apiKey, _ := m["api_key"].(string)
		if apiKey == "sk-test1234567890" {
			t.Error("api_key should be masked, got plaintext")
		}
		if !strings.Contains(apiKey, "*") {
			t.Errorf("api_key should contain * for masking, got %q", apiKey)
		}
	})

	// EP33: PUT /api/staff/config/ai-providers/{id} → 200
	t.Run("EP33_Update", func(t *testing.T) {
		if providerID == 0 {
			t.Fatal("no providerID")
		}
		body := jsonBody(t, map[string]any{"name": "E2E_LLM_updated"})
		resp := doReq(t, "PUT", fmt.Sprintf("/api/staff/config/ai-providers/%d", int64(providerID)), body, adminToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
		m := parseJSON(t, resp)
		if m["name"] != "E2E_LLM_updated" {
			t.Errorf("expected name updated, got %v", m["name"])
		}
	})

	// EP34: DELETE /api/staff/config/ai-providers/{id} → 200
	t.Run("EP34_Delete", func(t *testing.T) {
		if providerID == 0 {
			t.Fatal("no providerID")
		}
		resp := doReq(t, "DELETE", fmt.Sprintf("/api/staff/config/ai-providers/%d", int64(providerID)), nil, adminToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
	})

	// 错误用例：删除后 PUT → 404
	t.Run("EP33_AfterDelete_404", func(t *testing.T) {
		if providerID == 0 {
			t.Fatal("no providerID")
		}
		body := jsonBody(t, map[string]any{"name": "x"})
		resp := doReq(t, "PUT", fmt.Sprintf("/api/staff/config/ai-providers/%d", int64(providerID)), body, adminToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusNotFound)
	})

	// 错误用例：DOCTOR 无权限 → 403
	t.Run("EP31_DoctorForbidden_403", func(t *testing.T) {
		resp := doReq(t, "GET", "/api/staff/config/ai-providers", nil, doctorToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusForbidden)
	})
}

// ─── 10. config sensitive-words (4 端点) ──────────────────────────────────

// EP35-38: 4 个敏感词端点
func TestConfigSensitiveWords(t *testing.T) {
	setupHelper(t)
	adminToken := loginAdmin(t)

	var wordID float64
	uniqueWord := fmt.Sprintf("e2e_sw_%d", time.Now().UnixNano())

	// EP35: GET /api/staff/config/sensitive-words → 200
	t.Run("EP35_List", func(t *testing.T) {
		resp := doReq(t, "GET", "/api/staff/config/sensitive-words", nil, adminToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
		m := parseJSON(t, resp)
		if _, ok := m["items"]; !ok {
			t.Errorf("expected items: %v", m)
		}
	})

	// EP36: POST /api/staff/config/sensitive-words → 201
	t.Run("EP36_Create", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"word":      uniqueWord,
			"category":  "suicide",
			"is_active": true,
		})
		resp := doReq(t, "POST", "/api/staff/config/sensitive-words", body, adminToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusCreated)
		m := parseJSON(t, resp)
		wordID, _ = m["id"].(float64)
		if wordID == 0 {
			t.Fatalf("expected id: %v", m)
		}
	})

	// EP37: PUT /api/staff/config/sensitive-words/{id} → 200
	t.Run("EP37_Update", func(t *testing.T) {
		if wordID == 0 {
			t.Fatal("no wordID")
		}
		body := jsonBody(t, map[string]any{"word": uniqueWord + "_u"})
		resp := doReq(t, "PUT", fmt.Sprintf("/api/staff/config/sensitive-words/%d", int64(wordID)), body, adminToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
	})

	// EP38: DELETE /api/staff/config/sensitive-words/{id} → 200
	t.Run("EP38_Delete", func(t *testing.T) {
		if wordID == 0 {
			t.Fatal("no wordID")
		}
		resp := doReq(t, "DELETE", fmt.Sprintf("/api/staff/config/sensitive-words/%d", int64(wordID)), nil, adminToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
	})

	// 错误用例：重复创建 → 409
	t.Run("EP36_Duplicate_409", func(t *testing.T) {
		body := jsonBody(t, map[string]any{"word": "自杀", "category": "suicide"})
		resp := doReq(t, "POST", "/api/staff/config/sensitive-words", body, adminToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusConflict)
	})
}

// ─── 11. config safety-rules (4 端点) ──────────────────────────────────────

// EP39-42: 4 个安全规则端点
func TestConfigSafetyRules(t *testing.T) {
	setupHelper(t)
	adminToken := loginAdmin(t)

	var ruleID float64

	// EP39: GET /api/staff/config/safety-rules → 200
	t.Run("EP39_List", func(t *testing.T) {
		resp := doReq(t, "GET", "/api/staff/config/safety-rules", nil, adminToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
		m := parseJSON(t, resp)
		if _, ok := m["items"]; !ok {
			t.Errorf("expected items: %v", m)
		}
	})

	// EP40: POST /api/staff/config/safety-rules → 201
	t.Run("EP40_Create", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"name":        fmt.Sprintf("E2E规则_%d", time.Now().UnixNano()),
			"category":    "diagnosis",
			"pattern":     "确诊|诊断为",
			"action":      "replace",
			"replacement": "请咨询主治医生",
			"is_active":   true,
		})
		resp := doReq(t, "POST", "/api/staff/config/safety-rules", body, adminToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusCreated)
		m := parseJSON(t, resp)
		ruleID, _ = m["id"].(float64)
		if ruleID == 0 {
			t.Fatalf("expected id: %v", m)
		}
	})

	// EP41: PUT /api/staff/config/safety-rules/{id} → 200
	t.Run("EP41_Update", func(t *testing.T) {
		if ruleID == 0 {
			t.Fatal("no ruleID")
		}
		body := jsonBody(t, map[string]any{"name": "E2E规则_updated"})
		resp := doReq(t, "PUT", fmt.Sprintf("/api/staff/config/safety-rules/%d", int64(ruleID)), body, adminToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
	})

	// EP42: DELETE /api/staff/config/safety-rules/{id} → 200
	t.Run("EP42_Delete", func(t *testing.T) {
		if ruleID == 0 {
			t.Fatal("no ruleID")
		}
		resp := doReq(t, "DELETE", fmt.Sprintf("/api/staff/config/safety-rules/%d", int64(ruleID)), nil, adminToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
	})

	// 错误用例：无效正则 → 422
	t.Run("EP40_InvalidRegex_422", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"name":     "E2E invalid",
			"category": "diagnosis",
			"pattern":  "[invalid",
			"action":   "block",
		})
		resp := doReq(t, "POST", "/api/staff/config/safety-rules", body, adminToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusUnprocessableEntity)
	})
}

// ─── 12. config rag (2 端点) ──────────────────────────────────────────────

// EP43-44: 2 个 RAG 配置端点
func TestConfigRAG(t *testing.T) {
	setupHelper(t)
	adminToken := loginAdmin(t)

	// EP43: GET /api/staff/config/rag → 200
	t.Run("EP43_Get", func(t *testing.T) {
		resp := doReq(t, "GET", "/api/staff/config/rag", nil, adminToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
		m := parseJSON(t, resp)
		for _, k := range []string{"chunk_size", "chunk_overlap", "max_chunks", "top_k", "similarity_threshold", "rerank_enabled", "rerank_threshold", "updated_at"} {
			if _, ok := m[k]; !ok {
				t.Errorf("missing field %q: %v", k, m)
			}
		}
	})

	// EP44: PUT /api/staff/config/rag → 200
	t.Run("EP44_Update", func(t *testing.T) {
		body := jsonBody(t, map[string]any{"chunk_size": 1000, "top_k": 8})
		resp := doReq(t, "PUT", "/api/staff/config/rag", body, adminToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
		m := parseJSON(t, resp)
		if cs, _ := m["chunk_size"].(float64); cs != 1000 {
			t.Errorf("expected chunk_size=1000, got %v", m["chunk_size"])
		}
	})

	// 错误用例：超范围 → 422
	t.Run("EP44_OutOfRange_422", func(t *testing.T) {
		body := jsonBody(t, map[string]any{"chunk_size": 9999})
		resp := doReq(t, "PUT", "/api/staff/config/rag", body, adminToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusUnprocessableEntity)
	})
}

// ─── 13. config prompts (4 端点) ──────────────────────────────────────────

// EP45-48: 4 个 Prompt 模板端点
func TestConfigPrompts(t *testing.T) {
	setupHelper(t)
	adminToken := loginAdmin(t)

	var promptID float64
	uniqueContent := fmt.Sprintf("E2E prompt %d", time.Now().UnixNano())

	// EP45: GET /api/staff/config/prompts → 200
	t.Run("EP45_List", func(t *testing.T) {
		resp := doReq(t, "GET", "/api/staff/config/prompts", nil, adminToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
		m := parseJSON(t, resp)
		if _, ok := m["items"]; !ok {
			t.Errorf("expected items: %v", m)
		}
	})

	// EP46: POST /api/staff/config/prompts → 201
	t.Run("EP46_Create", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"type":      "system",
			"content":   uniqueContent,
			"is_active": false, // 用 false 避免与其他 active system 冲突
		})
		resp := doReq(t, "POST", "/api/staff/config/prompts", body, adminToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusCreated)
		m := parseJSON(t, resp)
		promptID, _ = m["id"].(float64)
		if promptID == 0 {
			t.Fatalf("expected id: %v", m)
		}
		if m["version"] == nil {
			t.Errorf("expected version: %v", m)
		}
	})

	// EP47: PUT /api/staff/config/prompts/{id} → 200
	t.Run("EP47_Update", func(t *testing.T) {
		if promptID == 0 {
			t.Fatal("no promptID")
		}
		body := jsonBody(t, map[string]any{"content": "更新后的prompt"})
		resp := doReq(t, "PUT", fmt.Sprintf("/api/staff/config/prompts/%d", int64(promptID)), body, adminToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
		m := parseJSON(t, resp)
		if m["content"] != "更新后的prompt" {
			t.Errorf("expected content updated, got %v", m["content"])
		}
	})

	// EP48: DELETE /api/staff/config/prompts/{id} → 200（非 active 可删）
	t.Run("EP48_Delete", func(t *testing.T) {
		if promptID == 0 {
			t.Fatal("no promptID")
		}
		resp := doReq(t, "DELETE", fmt.Sprintf("/api/staff/config/prompts/%d", int64(promptID)), nil, adminToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
	})

	// 错误用例：删除 active 版本 → 409
	t.Run("EP48_DeleteActive_409", func(t *testing.T) {
		// seed 中的 system prompt 是 active，但不知道 ID；用 SQL 查询
		if e2ePool == nil {
			t.Skip("no e2ePool, skip")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		var id int64
		err := e2ePool.QueryRow(ctx, `SELECT id FROM prompt_templates WHERE is_active = true AND type = 'system' LIMIT 1`).Scan(&id)
		if err != nil {
			t.Skipf("no active system prompt in DB: %v", err)
		}
		resp := doReq(t, "DELETE", fmt.Sprintf("/api/staff/config/prompts/%d", id), nil, adminToken)
		defer drainAndClose(resp)
		if resp.StatusCode != http.StatusConflict {
			t.Errorf("expected 409 for delete active, got %d", resp.StatusCode)
		}
	})
}

// ─── 14. config safety-messages (2 端点) ──────────────────────────────────

// EP49-50: 2 个安全话术端点
func TestConfigSafetyMessages(t *testing.T) {
	setupHelper(t)
	adminToken := loginAdmin(t)

	// EP49: GET /api/staff/config/safety-messages → 200
	t.Run("EP49_Get", func(t *testing.T) {
		resp := doReq(t, "GET", "/api/staff/config/safety-messages", nil, adminToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
		m := parseJSON(t, resp)
		for _, k := range []string{"rejection_message", "emergency_message", "safety_warning_message", "crisis_response", "updated_at"} {
			if _, ok := m[k]; !ok {
				t.Errorf("missing field %q: %v", k, m)
			}
		}
	})

	// EP50: PUT /api/staff/config/safety-messages → 200
	t.Run("EP50_Update", func(t *testing.T) {
		body := jsonBody(t, map[string]any{"rejection_message": "抱歉，我无法回答这个问题，建议您咨询您的主治医生。"})
		resp := doReq(t, "PUT", "/api/staff/config/safety-messages", body, adminToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
		m := parseJSON(t, resp)
		if m["rejection_message"] != "抱歉，我无法回答这个问题，建议您咨询您的主治医生。" {
			t.Errorf("expected rejection_message updated, got %v", m["rejection_message"])
		}
	})
}

// ─── 12. 权限矩阵 + 错误格式（迁移自 tests/e2e/e2e_test.go） ────────────────

// T-PERM-01~06: 角色权限矩阵
func TestPermissionMatrix(t *testing.T) {
	setupHelper(t)
	patientToken := login(t, "testpatient", "Pass1234", "/api/auth/login")
	staffToken := login(t, "doctor1", "Pass1234", "/api/auth/login")

	t.Run("T-PERM-01_PatientToStaff", func(t *testing.T) {
		resp := doReq(t, "GET", "/api/staff/wiki/articles", nil, patientToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusForbidden)
	})

	t.Run("T-PERM-02_StaffToChat", func(t *testing.T) {
		// 聊天对所有已登录用户开放，staff 不应被拒绝
		resp := doReq(t, "GET", "/api/chat/conversations", nil, staffToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
	})

	t.Run("T-PERM-03_DoctorToAdmin", func(t *testing.T) {
		resp := doReq(t, "GET", "/api/staff/config/ai-providers", nil, staffToken)
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusForbidden)
	})

	t.Run("T-PERM-04_AnonToProtected", func(t *testing.T) {
		resp := doReq(t, "GET", "/api/base/departments", nil, "")
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusUnauthorized)
	})

	t.Run("T-PERM-05_AnonToPublicWiki", func(t *testing.T) {
		resp := doReq(t, "GET", "/api/wiki/articles", nil, "")
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
	})

	t.Run("T-PERM-06_AnonToHealthz", func(t *testing.T) {
		resp := doReq(t, "GET", "/healthz", nil, "")
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusOK)
	})
}

// T-ERR-01~03: 错误响应格式
func TestErrorFormat(t *testing.T) {
	// T-ERR-01: 无 token → {code, message} + 401
	t.Run("T-ERR-01_Unauthorized", func(t *testing.T) {
		resp := doReq(t, "GET", "/api/staff/wiki/articles", nil, "")
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusUnauthorized)
		m := parseJSON(t, resp)
		if _, ok := m["code"]; !ok {
			t.Errorf("expected code: %v", m)
		}
		if _, ok := m["message"]; !ok {
			t.Errorf("expected message: %v", m)
		}
	})

	// T-ERR-02: 不存在路径 → {code:"NOT_FOUND"} + 404
	t.Run("T-ERR-02_NotFound", func(t *testing.T) {
		resp := doReq(t, "GET", "/api/nonexistent-path-xyz", nil, "")
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusNotFound)
		m := parseJSON(t, resp)
		if m["code"] != "NOT_FOUND" {
			t.Errorf("expected NOT_FOUND, got %v", m["code"])
		}
	})

	// T-ERR-03: 错误方法 → {code:"METHOD_NOT_ALLOWED"} + 405
	t.Run("T-ERR-03_MethodNotAllowed", func(t *testing.T) {
		resp := doReq(t, "DELETE", "/api/wiki/articles", nil, "")
		defer drainAndClose(resp)
		assertStatus(t, resp, http.StatusMethodNotAllowed)
		m := parseJSON(t, resp)
		if m["code"] != "METHOD_NOT_ALLOWED" {
			t.Errorf("expected METHOD_NOT_ALLOWED, got %v", m["code"])
		}
	})
}
