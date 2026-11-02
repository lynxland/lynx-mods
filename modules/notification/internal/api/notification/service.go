// Package notification 是通知模块的 API 层：gRPC 处理器与装配入口
// （Module/Assemble）。
package notification

import (
	"context"
	"errors"

	notificationv1 "github.com/lynxland/lynx-mods/modules/notification/genproto/notification/v1"
	appnotification "github.com/lynxland/lynx-mods/modules/notification/internal/app/notification"

	"github.com/lynxland/grpcapi/principal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/lynxland/lynx-mods/modules/notification/domain/notification"
)

// Service 实现 NotificationService（薄转发：收件人一律取自认证语境，
// 不信任请求参数；业务在用例层）。
type Service struct {
	notificationv1.UnimplementedNotificationServiceServer
	uc *appnotification.UseCase
}

// NewService 构造处理器。
func NewService(uc *appnotification.UseCase) *Service { return &Service{uc: uc} }

// actorID 从语境取当前认证主体（proto 注解已要求认证，此处兜底）。
func actorID(ctx context.Context) (string, error) {
	p, ok := principal.From(ctx)
	if !ok || p.ID == "" {
		return "", status.Error(codes.Unauthenticated, "authentication required")
	}
	return p.ID, nil
}

// ListNotifications 处理自助列表（分页 + 未读过滤 + 未读计数）。
func (s *Service) ListNotifications(ctx context.Context, req *notificationv1.ListNotificationsRequest) (*notificationv1.ListNotificationsResponse, error) {
	who, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	res, err := s.uc.List(ctx, who, req.GetUnreadOnly(), req.GetPageSize(), req.GetPageToken())
	if err != nil {
		return nil, toStatus(err)
	}
	out := &notificationv1.ListNotificationsResponse{
		NextPageToken: res.NextToken,
		TotalSize:     res.TotalSize,
		UnreadSize:    res.UnreadSize,
	}
	for _, n := range res.Notifications {
		out.Notifications = append(out.Notifications, toAPI(n))
	}
	return out, nil
}

// MarkNotificationRead 处理单条已读（仅本人的通知；幂等）。
func (s *Service) MarkNotificationRead(ctx context.Context, req *notificationv1.MarkNotificationReadRequest) (*emptypb.Empty, error) {
	who, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.uc.MarkRead(ctx, who, req.GetNotificationId()); err != nil {
		return nil, toStatus(err)
	}
	return &emptypb.Empty{}, nil
}

// MarkAllNotificationsRead 处理全部已读。
func (s *Service) MarkAllNotificationsRead(ctx context.Context, _ *notificationv1.MarkAllNotificationsReadRequest) (*notificationv1.MarkAllNotificationsReadResponse, error) {
	who, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	updated, err := s.uc.MarkAllRead(ctx, who)
	if err != nil {
		return nil, toStatus(err)
	}
	return &notificationv1.MarkAllNotificationsReadResponse{Updated: updated}, nil
}

// toStatus 把领域哨兵错误映射为携带 ErrorInfo 的 grpc status
// （torchwood P3-18 取舍：app/api 层允许直接使用 grpc/status）。
func toStatus(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, notification.ErrNotificationNotFound):
		return status.New(codes.NotFound, err.Error()).Err()
	default:
		return err
	}
}

func toAPI(n *notification.Notification) *notificationv1.Notification {
	out := &notificationv1.Notification{
		Id:        n.ID,
		Type:      n.Type,
		Title:     n.Title,
		Body:      n.Body,
		Payload:   n.Payload,
		CreatedAt: timestamppb.New(n.CreatedAt),
	}
	if n.ReadAt != nil {
		out.ReadAt = timestamppb.New(*n.ReadAt)
	}
	return out
}
