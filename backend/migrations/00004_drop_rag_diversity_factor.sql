-- +goose Up
-- +goose StatementBegin

-- 移除 MMR 多样性层配置项（diversity_factor）。
-- 理由：该层默认关闭（0），且用 bigram Jaccard 覆盖 rerank 神经排序属"弱信号覆盖强信号"；
-- 无 eval 支撑召回增益，属冗余配置。DROP COLUMN 会自动带掉其 CHECK 约束 rag_diversity_factor_chk。
ALTER TABLE rag_configs
    DROP COLUMN IF EXISTS diversity_factor;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE rag_configs
    ADD COLUMN diversity_factor NUMERIC(4,3) NOT NULL DEFAULT 0.000,
    ADD CONSTRAINT rag_diversity_factor_chk CHECK (diversity_factor BETWEEN 0 AND 1);

-- +goose StatementEnd