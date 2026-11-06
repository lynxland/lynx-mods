-- +goose Up
-- account 模块的初始迁移（ADR-0009：identity+session+access 合并为一个
-- 产品能力单元）。逐段继承自三模块各自的 00001：全部 IF NOT EXISTS——
-- 已装存量应用（旧三模块形态）升级重装时数据零迁移；goose 版本表
-- goose_account_db 从新起，旧 goose_identity_db / goose_session_db /
-- goose_access_db 残留无害（不再被引用，可手工清理）。
-- 表名保留原样（identity_users / session_sessions / access_role_assignments）
-- ——表名是存量应用的数据资产，不随模块改名。

-- —— 用户（identity 域）——
CREATE TABLE IF NOT EXISTS identity_users (
    id            text PRIMARY KEY,
    email         text NOT NULL,
    password_hash text NOT NULL,
    display_name  text NOT NULL,
    status        text NOT NULL DEFAULT 'active',
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
-- 归一化（小写）由领域层强制；唯一索引保证兜底。
CREATE UNIQUE INDEX IF NOT EXISTS idx_identity_users_email ON identity_users (email);

-- —— 会话（session 域）——
CREATE TABLE IF NOT EXISTS session_sessions (
    id                text PRIMARY KEY,
    user_id           text NOT NULL,
    refresh_jti       text NOT NULL,
    status            text NOT NULL DEFAULT 'active',
    created_at        timestamptz NOT NULL DEFAULT now(),
    last_refreshed_at timestamptz NOT NULL DEFAULT now(),
    expires_at        timestamptz NOT NULL,
    revoked_at        timestamptz,
    revoke_reason     text
);
CREATE INDEX IF NOT EXISTS idx_session_sessions_user ON session_sessions (user_id);
-- 契约④：不设指向 identity_users 的外键（迁移链隔离）。

-- —— 角色分配（access 域）——
CREATE TABLE IF NOT EXISTS access_role_assignments (
    user_id    text NOT NULL,
    role       text NOT NULL,
    granted_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, role)
);
CREATE INDEX IF NOT EXISTS idx_access_role_assignments_user ON access_role_assignments (user_id);
-- 契约④：user_id 只存 ID，不设指向 identity_users 的外键（迁移链隔离）。
-- 角色目录（名字→权限集）在配置段声明，不落库。

-- +goose Down
DROP TABLE IF EXISTS access_role_assignments;
DROP TABLE IF EXISTS session_sessions;
DROP TABLE IF EXISTS identity_users;
