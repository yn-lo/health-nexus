-- ============================================================================
-- seed.dev.sql — 开发/E2E 测试数据（幂等）
-- ----------------------------------------------------------------------------
-- 加载条件：仅在 config.env = development（默认）时由 ApplySchema 应用；
--   env 为其他值（如 production）时整个文件不加载 → 数据库保持干净。
--   环境变量：HEALTH_NEXUS_ENV
--
-- 用途：为前端 Playwright E2E（frontend/tests/e2e）提供固定账号与科室绑定。
--   医护账号必须绑定主科室（is_primary=true），否则危机事件/反馈统计等
--   按科室隔离的接口会因 DeptID=0 返回 403（见 chat/service 的科室越权守卫）。
--
-- 凭据：所有测试账号密码统一为 password123（argon2id 哈希，仅用于开发环境）。
--   生产环境切勿加载本文件。
--
-- 幂等：全部语句用 ON CONFLICT / WHERE NOT EXISTS 守卫，可安全反复重放。
-- ============================================================================

-- ----------------------------------------------------------------------------
-- 测试科室（供账号绑定；name 无唯一约束，用 WHERE NOT EXISTS 守卫幂等）
-- ----------------------------------------------------------------------------
INSERT INTO departments (name, is_public, is_active, description)
SELECT '全科医学科', TRUE, TRUE, '演示科室：E2E 测试账号归属'
WHERE NOT EXISTS (SELECT 1 FROM departments WHERE name = '全科医学科');

INSERT INTO departments (name, is_public, is_active, description)
SELECT '心血管内科', TRUE, TRUE, '演示科室：E2E 测试账号归属'
WHERE NOT EXISTS (SELECT 1 FROM departments WHERE name = '心血管内科');

-- ----------------------------------------------------------------------------
-- 测试账号（密码统一 password123）
-- admin 单独用 DO UPDATE：seed.sql 先用 HEALTH_NEXUS_SEED_ADMIN_PASSWORD 建了 admin
--   （ON CONFLICT DO NOTHING，不覆盖），但 E2E 期望开发环境统一用 password123，
--   故此处显式改写其密码哈希（仅 development 生效，生产不加载本文件）。
-- ----------------------------------------------------------------------------
INSERT INTO users (username, role, password_hash) VALUES
    ('admin',       'SUPER_ADMIN', 'argon2id$v=19$m=65536,t=3,p=2$kN/oUNYeJzUbqo018HHiQA==$p2gCDZa+vNTtsYYlSkoIbyQaA1Qu2rqEJfXLEbvLd8Y=')
ON CONFLICT (username) DO UPDATE SET password_hash = EXCLUDED.password_hash;

INSERT INTO users (username, role, password_hash) VALUES
    ('dept_admin',  'DEPT_ADMIN',  'argon2id$v=19$m=65536,t=3,p=2$kN/oUNYeJzUbqo018HHiQA==$p2gCDZa+vNTtsYYlSkoIbyQaA1Qu2rqEJfXLEbvLd8Y='),
    ('doctor_zhang','DOCTOR',      'argon2id$v=19$m=65536,t=3,p=2$kN/oUNYeJzUbqo018HHiQA==$p2gCDZa+vNTtsYYlSkoIbyQaA1Qu2rqEJfXLEbvLd8Y='),
    ('doctor_wang', 'DOCTOR',      'argon2id$v=19$m=65536,t=3,p=2$kN/oUNYeJzUbqo018HHiQA==$p2gCDZa+vNTtsYYlSkoIbyQaA1Qu2rqEJfXLEbvLd8Y='),
    ('nurse_liu',   'NURSE',       'argon2id$v=19$m=65536,t=3,p=2$kN/oUNYeJzUbqo018HHiQA==$p2gCDZa+vNTtsYYlSkoIbyQaA1Qu2rqEJfXLEbvLd8Y='),
    ('patient_li',  'PATIENT',     'argon2id$v=19$m=65536,t=3,p=2$kN/oUNYeJzUbqo018HHiQA==$p2gCDZa+vNTtsYYlSkoIbyQaA1Qu2rqEJfXLEbvLd8Y='),
    ('patient_wang','PATIENT',     'argon2id$v=19$m=65536,t=3,p=2$kN/oUNYeJzUbqo018HHiQA==$p2gCDZa+vNTtsYYlSkoIbyQaA1Qu2rqEJfXLEbvLd8Y='),
    ('patient_zhao','PATIENT',     'argon2id$v=19$m=65536,t=3,p=2$kN/oUNYeJzUbqo018HHiQA==$p2gCDZa+vNTtsYYlSkoIbyQaA1Qu2rqEJfXLEbvLd8Y=')
ON CONFLICT (username) DO NOTHING;

-- ----------------------------------------------------------------------------
-- 医护账号绑定主科室（E2E 依赖：缺失则按科室隔离的接口返回 403）
-- 全科医学科承载 dept_admin/nurse_liu/patient_*，心血管内科承载两位医生。
-- ----------------------------------------------------------------------------
INSERT INTO user_departments (user_id, department_id, is_primary)
SELECT u.id, d.id, TRUE
FROM users u
JOIN departments d ON d.name = CASE
    WHEN u.username IN ('doctor_zhang', 'doctor_wang') THEN '心血管内科'
    ELSE '全科医学科'
END
WHERE u.username IN ('dept_admin', 'doctor_zhang', 'doctor_wang', 'nurse_liu')
ON CONFLICT (user_id, department_id) DO NOTHING;

-- 患者账号归属全科医学科（非主科室，仅用于演示归属关系）
INSERT INTO user_departments (user_id, department_id, is_primary)
SELECT u.id, d.id, FALSE
FROM users u
JOIN departments d ON d.name = '全科医学科'
WHERE u.username IN ('patient_li', 'patient_wang', 'patient_zhao')
ON CONFLICT (user_id, department_id) DO NOTHING;
