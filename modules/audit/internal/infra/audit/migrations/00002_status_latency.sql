-- +goose Up
-- v0.7.0：拦截器记录面补全（gRPC 状态码 + 耗时）。
ALTER TABLE audit_records ADD COLUMN IF NOT EXISTS status_code text NOT NULL DEFAULT '';
ALTER TABLE audit_records ADD COLUMN IF NOT EXISTS latency_millis bigint NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE audit_records DROP COLUMN IF EXISTS latency_millis;
ALTER TABLE audit_records DROP COLUMN IF EXISTS status_code;
