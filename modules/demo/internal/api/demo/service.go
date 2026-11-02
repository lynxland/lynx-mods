// Package demo 是样例模块的 API 层：gRPC 处理器与装配入口（Module/Assemble）。
package demo

import (
	"context"
	"errors"

	demov1 "github.com/lynxland/lynx-mods/modules/demo/genproto/demo/v1"
	appdemo "github.com/lynxland/lynx-mods/modules/demo/internal/app/demo"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/lynxland/lynx-mods/modules/demo/domain/demo"
)

// Service 实现 DemoService（薄转发：参数校验交给 protovalidate，业务在用例层）。
type Service struct {
	demov1.UnimplementedDemoServiceServer
	uc *appdemo.UseCase
}

// NewService 构造处理器。
func NewService(uc *appdemo.UseCase) *Service { return &Service{uc: uc} }

// CreateItem 处理创建。
func (s *Service) CreateItem(ctx context.Context, req *demov1.CreateItemRequest) (*demov1.Item, error) {
	item, err := s.uc.Create(ctx, req.GetTitle())
	if err != nil {
		return nil, toStatus(err)
	}
	return toAPI(item), nil
}

// GetItem 处理查询。
func (s *Service) GetItem(ctx context.Context, req *demov1.GetItemRequest) (*demov1.Item, error) {
	item, err := s.uc.Get(ctx, req.GetId())
	if err != nil {
		return nil, toStatus(err)
	}
	return toAPI(item), nil
}

// ListItems 处理列表。
func (s *Service) ListItems(ctx context.Context, req *demov1.ListItemsRequest) (*demov1.ListItemsResponse, error) {
	res, err := s.uc.List(ctx, req.GetPageSize(), req.GetPageToken())
	if err != nil {
		return nil, toStatus(err)
	}
	out := &demov1.ListItemsResponse{
		NextPageToken: res.NextToken,
		TotalSize:     res.TotalSize,
	}
	for _, item := range res.Items {
		out.Items = append(out.Items, toAPI(item))
	}
	return out, nil
}

// RenameItem 处理改名。
func (s *Service) RenameItem(ctx context.Context, req *demov1.RenameItemRequest) (*demov1.Item, error) {
	item, err := s.uc.Rename(ctx, req.GetId(), req.GetTitle())
	if err != nil {
		return nil, toStatus(err)
	}
	return toAPI(item), nil
}

// DeleteItem 处理删除。
func (s *Service) DeleteItem(ctx context.Context, req *demov1.DeleteItemRequest) (*emptypb.Empty, error) {
	if err := s.uc.Delete(ctx, req.GetId()); err != nil {
		return nil, toStatus(err)
	}
	return &emptypb.Empty{}, nil
}

// toStatus 把领域哨兵错误映射为携带 ErrorInfo 的 grpc status
// （torchwood P3-18 取舍：app/api 层允许直接使用 grpc/status）。
func toStatus(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, demo.ErrItemNotFound):
		return status.New(codes.NotFound, err.Error()).Err()
	case errors.Is(err, demo.ErrEmptyTitle):
		return status.New(codes.InvalidArgument, err.Error()).Err()
	default:
		return err
	}
}

func toAPI(item *demo.Item) *demov1.Item {
	return &demov1.Item{
		Id:        item.ID,
		Title:     item.Title,
		CreatedAt: timestamppb.New(item.CreatedAt),
		UpdatedAt: timestamppb.New(item.UpdatedAt),
	}
}
