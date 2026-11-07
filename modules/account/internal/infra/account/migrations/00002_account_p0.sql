-- +goose Up
-- v0.7.0 Product Ready P0：邮箱验证 + 会话设备元数据。
ALTER TABLE identity_users ADD COLUMN IF NOT EXISTS email_verified boolean NOT NULL DEFAULT false;
ALTER TABLE session_sessions ADD COLUMN IF NOT EXISTS user_agent text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE session_sessions DROP COLUMN IF EXISTS user_agent;
ALTER TABLE identity_users DROP COLUMN IF EXISTS email_verified;
