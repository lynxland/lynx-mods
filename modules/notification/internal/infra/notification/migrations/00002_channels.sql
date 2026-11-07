-- +goose Up
-- v0.7.0 渠道化：投递偏好（复合键 user_id+type+channel；未配置=启用）。
CREATE TABLE IF NOT EXISTS notification_preferences (
    user_id  text NOT NULL,
    type     text NOT NULL,
    channel  text NOT NULL,
    enabled  boolean NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, type, channel)
);

-- +goose Down
DROP TABLE IF EXISTS notification_preferences;
