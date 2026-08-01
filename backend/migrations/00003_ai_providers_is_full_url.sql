-- +goose Up
-- +goose StatementBegin

-- ai_providers 增加"完整链接"开关：为 true 时后端对 api_url 原样使用，
-- 不再自动拼接 /v1（兼容智谱 /api/paas/v4 等非 /v1 版本层的 OpenAI 兼容 API）。
ALTER TABLE ai_providers
    ADD COLUMN is_full_url BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE ai_providers
    DROP COLUMN IF EXISTS is_full_url;

-- +goose StatementEnd
