// Package notification 是通知模块的用例层：编排领域与端口。写入仅经
// 事件订阅（watcher 调 OnXxx）——内容在发生时刻定格为快照（title/body
// 文案是产品可见字符串，装后即归应用，按需修改）；查询/已读是收件人
// 自助操作（user_id 一律取自调用方语境或事件载荷，不信任请求参数）。
package notification

import (
	"context"
	"fmt"
	"time"

	"github.com/lynxland/appkit/crud"
	"github.com/lynxland/appkit/idgen"
	"github.com/lynxland/lynx-start/modules/notification/domain/notification"
)

// UseCase 是通知模块的用例集。
type UseCase struct {
	repo notification.Repository
	now  func() time.Time
}

// New 构造用例（Assemble 调用）。
func New(repo notification.Repository) *UseCase {
	return &UseCase{repo: repo, now: time.Now}
}

// ListResult 是通知列表用例的产物。
type ListResult struct {
	Notifications []*notification.Notification
	NextToken     string
	TotalSize     int32
	UnreadSize    int32
}

// List 列收件人通知（分页；unreadOnly 仅未读），附带整户未读计数。
func (u *UseCase) List(ctx context.Context, userID string, unreadOnly bool, pageSize int32, pageToken string) (*ListResult, error) {
	params, err := crud.ParseListParams(pageSize, pageToken, "", "")
	if err != nil {
		return nil, err
	}
	items, total, err := u.repo.ListByUser(ctx, userID, notification.Query{
		PageSize: int(params.PageSize), Offset: params.Offset, UnreadOnly: unreadOnly,
	})
	if err != nil {
		return nil, err
	}
	page, err := crud.FinalizeOffsetPage(params, total, len(items))
	if err != nil {
		return nil, err
	}
	unread, err := u.repo.CountUnread(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &ListResult{
		Notifications: items, NextToken: page.NextToken,
		TotalSize: int32(total), UnreadSize: int32(unread),
	}, nil
}

// MarkRead 标记单条已读（他人通知与不存在统一 ErrNotificationNotFound——
// 不泄露存在性差异；幂等）。
func (u *UseCase) MarkRead(ctx context.Context, userID, id string) error {
	if _, err := u.repo.MarkRead(ctx, id, userID, u.now().UTC()); err != nil {
		return err
	}
	return nil
}

// MarkAllRead 全部未读置已读，返回标记条数。
func (u *UseCase) MarkAllRead(ctx context.Context, userID string) (int64, error) {
	return u.repo.MarkAllRead(ctx, userID, u.now().UTC())
}

// OnPasswordChanged 生成改密安全提醒（收件人 = 事件载荷里的用户）。
func (u *UseCase) OnPasswordChanged(ctx context.Context, userID string) error {
	return u.create(ctx, userID, notification.IdentityUserPasswordChanged,
		"密码已更改", "你的密码刚刚被更改。如果不是你本人操作，请立即重置密码。",
		map[string]string{"id": userID},
	)
}

// OnMemberAdded 生成「被加入组织」通知。
func (u *UseCase) OnMemberAdded(ctx context.Context, userID, organizationID, organizationName string) error {
	return u.create(ctx, userID, notification.OrganizationMemberAdded,
		fmt.Sprintf("你已被加入组织 %s", organizationName),
		fmt.Sprintf("你已被组织 %s 添加为成员。", organizationName),
		map[string]string{"organization_id": organizationID, "user_id": userID},
	)
}

// OnMemberRemoved 生成「被移出组织」通知。
func (u *UseCase) OnMemberRemoved(ctx context.Context, userID, organizationID, organizationName string) error {
	return u.create(ctx, userID, notification.OrganizationMemberRemoved,
		fmt.Sprintf("你已被移出组织 %s", organizationName),
		fmt.Sprintf("你不再是组织 %s 的成员。", organizationName),
		map[string]string{"organization_id": organizationID, "user_id": userID},
	)
}

// create 构造聚合并落库。
func (u *UseCase) create(ctx context.Context, userID, typ, title, body string, payload map[string]string) error {
	n, err := notification.NewNotification(idgen.New(), userID, typ, title, body, payload, u.now().UTC())
	if err != nil {
		return err
	}
	return u.repo.Create(ctx, n)
}
