// Package task 是任务模块的 API 层：gRPC 处理器与装配入口
// （Module/Assemble）。
package task

import (
	"context"
	"errors"

	"github.com/lynxland/grpcapi/principal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	taskv1 "github.com/lynxland/lynx-mods/modules/task/genproto/task/v1"
	apptask "github.com/lynxland/lynx-mods/modules/task/internal/app/task"

	"github.com/lynxland/lynx-mods/modules/organization/domain/organization"
	"github.com/lynxland/lynx-mods/modules/task/domain/task"
)

// Service 实现 TaskService（薄转发：成员资格与创建者裁决在用例层，
// 此处只取认证主体）。
type Service struct {
	taskv1.UnimplementedTaskServiceServer
	uc *apptask.UseCase
}

// NewService 构造处理器。
func NewService(uc *apptask.UseCase) *Service { return &Service{uc: uc} }

// actorID 从语境取当前认证主体（proto 注解已要求认证，此处兜底）。
func actorID(ctx context.Context) (string, error) {
	p, ok := principal.From(ctx)
	if !ok || p.ID == "" {
		return "", status.Error(codes.Unauthenticated, "authentication required")
	}
	return p.ID, nil
}

// CreateTask 处理创建（成员即可创建）。
func (s *Service) CreateTask(ctx context.Context, req *taskv1.CreateTaskRequest) (*taskv1.Task, error) {
	who, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	t, err := s.uc.Create(ctx, who, req.GetOrganizationId(), req.GetTitle(), req.GetDescription())
	if err != nil {
		return nil, toStatus(err)
	}
	return toAPI(t), nil
}

// GetTask 处理查询（成员可见）。
func (s *Service) GetTask(ctx context.Context, req *taskv1.GetTaskRequest) (*taskv1.Task, error) {
	who, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	t, err := s.uc.Get(ctx, who, req.GetOrganizationId(), req.GetTaskId())
	if err != nil {
		return nil, toStatus(err)
	}
	return toAPI(t), nil
}

// ListTasks 处理组织任务列表。
func (s *Service) ListTasks(ctx context.Context, req *taskv1.ListTasksRequest) (*taskv1.ListTasksResponse, error) {
	who, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	res, err := s.uc.List(ctx, who, req.GetOrganizationId(), req.GetPageSize(), req.GetPageToken())
	if err != nil {
		return nil, toStatus(err)
	}
	out := &taskv1.ListTasksResponse{
		NextPageToken: res.NextToken,
		TotalSize:     res.TotalSize,
	}
	for _, t := range res.Tasks {
		out.Tasks = append(out.Tasks, toAPI(t))
	}
	return out, nil
}

// CompleteTask 处理完成（限创建者；幂等）。
func (s *Service) CompleteTask(ctx context.Context, req *taskv1.CompleteTaskRequest) (*taskv1.Task, error) {
	who, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	t, err := s.uc.Complete(ctx, who, req.GetOrganizationId(), req.GetTaskId())
	if err != nil {
		return nil, toStatus(err)
	}
	return toAPI(t), nil
}

// DeleteTask 处理删除（限创建者；幂等）。
func (s *Service) DeleteTask(ctx context.Context, req *taskv1.DeleteTaskRequest) (*emptypb.Empty, error) {
	who, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.uc.Delete(ctx, who, req.GetOrganizationId(), req.GetTaskId()); err != nil {
		return nil, toStatus(err)
	}
	return &emptypb.Empty{}, nil
}

// toStatus 把领域哨兵错误映射为携带 ErrorInfo 的 grpc status（跨模块
// 哨兵 organization.ErrNotMember 在此层映射——契约②允许 domain 级依赖）。
func toStatus(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, task.ErrTaskNotFound):
		return status.New(codes.NotFound, err.Error()).Err()
	case errors.Is(err, organization.ErrNotMember):
		return status.New(codes.PermissionDenied, err.Error()).Err()
	case errors.Is(err, task.ErrEmptyTitle):
		return status.New(codes.InvalidArgument, err.Error()).Err()
	case errors.Is(err, task.ErrNotCreator):
		return status.New(codes.PermissionDenied, err.Error()).Err()
	default:
		return err
	}
}

func toAPI(t *task.Task) *taskv1.Task {
	out := &taskv1.Task{
		Id:             t.ID,
		OrganizationId: t.OrganizationID,
		CreatorId:      t.CreatorID,
		Title:          t.Title,
		Description:    t.Description,
		CreatedAt:      timestamppb.New(t.CreatedAt),
		UpdatedAt:      timestamppb.New(t.UpdatedAt),
	}
	if t.CompletedAt != nil {
		out.CompletedAt = timestamppb.New(*t.CompletedAt)
	}
	return out
}
