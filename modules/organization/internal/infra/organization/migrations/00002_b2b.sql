-- +goose Up
-- v0.7.0 B2B 基本盘：成员角色分级（owner/admin/member）+ 邀请状态机 +
-- 组织软删。
ALTER TABLE organization_members ADD COLUMN IF NOT EXISTS role text NOT NULL DEFAULT 'member';
-- 存量回填：owner 行的角色对齐（role 为事实源，is_owner 保留为派生写）。
UPDATE organization_members SET role = 'owner' WHERE is_owner AND role = 'member';

CREATE TABLE IF NOT EXISTS organization_invitations (
    id              text PRIMARY KEY,
    organization_id text NOT NULL,
    email           text NOT NULL,
    role            text NOT NULL DEFAULT 'member',
    token           text NOT NULL UNIQUE,
    status          text NOT NULL DEFAULT 'pending',
    created_by      text NOT NULL,
    expires_at      timestamptz NOT NULL,
    accepted_at     timestamptz,
    accepted_by     text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
-- 同组织同邮箱仅一个 pending（重复邀请改写已有 pending 行）。
CREATE UNIQUE INDEX IF NOT EXISTS idx_org_invitations_pending
    ON organization_invitations (organization_id, email) WHERE status = 'pending';

ALTER TABLE organization_organizations ADD COLUMN IF NOT EXISTS deleted_at timestamptz;

-- +goose Down
ALTER TABLE organization_organizations DROP COLUMN IF EXISTS deleted_at;
DROP TABLE IF EXISTS organization_invitations;
ALTER TABLE organization_members DROP COLUMN IF EXISTS role;
