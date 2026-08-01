-- +goose Up
-- +goose StatementBegin

-- ============================================================================
-- 00002_seed_admin: 初始化默认超级管理员账号
-- 全新部署时由 server/worker 启动迁移自动执行（幂等，已存在则跳过）。
--
-- 默认凭据（登录后请立即在「个人中心 → 修改密码」更换）：
--   用户名: admin
--   密码:   Pass1234   （与测试环境约定一致；生产部署须通过管理后台改密）
--
-- 说明：password_hash 为 argon2id（m=65536,t=3,p=2）生成的固定哈希，
-- 仅作初始化凭据；不同环境请勿共用此账号的长期密码。
-- ============================================================================

INSERT INTO users (username, role, password_hash)
VALUES (
    'admin',
    'SUPER_ADMIN',
    'argon2id$v=19$m=65536,t=3,p=2$wWnyNI7XvpyqnZ4IslGZTA==$HdnL49YMReWiOYP1ebkpYILLTbKBHwS+gynKGz4RMw4='
)
ON CONFLICT (username) DO NOTHING;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DELETE FROM users WHERE username = 'admin' AND role = 'SUPER_ADMIN';

-- +goose StatementEnd
