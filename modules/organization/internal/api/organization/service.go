// Package organization 是组织模块的 API 层：gRPC 处理器与装配入口
// （Module/Assemble）。
package organization

import (
	"context"
	"errors"

	organizationv1 "github.com/lynxland/lynx-mods/modules/organization/genproto/organization/v1"
	apporganization "github.com/lynxland/lynx-mods/modules/organization/internal/app/organization"

	"github.com/lynxland/grpcapi/principal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/lynxland/lynx-mods/modules/organization/domain/organization"
)

// Service 实现 OrganizationService（薄转发：参数校验交给 protovalidate，
// 业务在用例层）。
type Service struct {
	organizationv1.UnimplementedOrganizationServiceServer
	uc *apporganization.UseCase
}

// NewService 构造处理器。
func NewService(uc *apporganization.UseCase) *Service { return &Service{uc: uc} }

// actorID 从语境取当前认证主体（proto 注解已要求认证，此处兜底）。
func actorID(ctx context.Context) (string, error) {
	p, ok := principal.From(ctx)
	if !ok || p.ID == "" {
		return "", status.Error(codes.Unauthenticated, "authentication required")
	}
	return p.ID, nil
}

// CreateOrganization 处理创建（创建者即 owner）。
func (s *Service) CreateOrganization(ctx context.Context, req *organizationv1.CreateOrganizationRequest) (*organizationv1.Organization, error) {
	who, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	o, err := s.uc.Create(ctx, who, req.GetName(), req.GetSlug())
	if err != nil {
		return nil, toStatus(err)
	}
	return toAPI(o), nil
}

// GetOrganization 处理查询（仅成员可见）。
func (s *Service) GetOrganization(ctx context.Context, req *organizationv1.GetOrganizationRequest) (*organizationv1.Organization, error) {
	who, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	o, err := s.uc.Get(ctx, who, req.GetOrganizationId())
	if err != nil {
		return nil, toStatus(err)
	}
	return toAPI(o), nil
}

// ListMyOrganizations 处理「我的组织」自助列表。
func (s *Service) ListMyOrganizations(ctx context.Context, req *organizationv1.ListMyOrganizationsRequest) (*organizationv1.ListMyOrganizationsResponse, error) {
	who, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	res, err := s.uc.ListMyOrganizations(ctx, who, req.GetPageSize(), req.GetPageToken())
	if err != nil {
		return nil, toStatus(err)
	}
	out := &organizationv1.ListMyOrganizationsResponse{
		NextPageToken: res.NextToken,
		TotalSize:     res.TotalSize,
	}
	for _, o := range res.Organizations {
		out.Organizations = append(out.Organizations, toAPI(o))
	}
	return out, nil
}

// RenameOrganization 处理改名（owner-only）。
func (s *Service) RenameOrganization(ctx context.Context, req *organizationv1.RenameOrganizationRequest) (*organizationv1.Organization, error) {
	who, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	o, err := s.uc.Rename(ctx, who, req.GetOrganizationId(), req.GetName())
	if err != nil {
		return nil, toStatus(err)
	}
	return toAPI(o), nil
}

// AddMember 处理加成员（owner-only；幂等）。
func (s *Service) AddMember(ctx context.Context, req *organizationv1.AddMemberRequest) (*emptypb.Empty, error) {
	who, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.uc.AddMember(ctx, who, req.GetOrganizationId(), req.GetUserId()); err != nil {
		return nil, toStatus(err)
	}
	return &emptypb.Empty{}, nil
}

// RemoveMember 处理移除成员（owner-only；不可移 owner；幂等）。
func (s *Service) RemoveMember(ctx context.Context, req *organizationv1.RemoveMemberRequest) (*emptypb.Empty, error) {
	who, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.uc.RemoveMember(ctx, who, req.GetOrganizationId(), req.GetUserId()); err != nil {
		return nil, toStatus(err)
	}
	return &emptypb.Empty{}, nil
}

// ListMembers 处理成员列表（成员可见）。
func (s *Service) ListMembers(ctx context.Context, req *organizationv1.ListMembersRequest) (*organizationv1.ListMembersResponse, error) {
	who, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	res, err := s.uc.ListMembers(ctx, who, req.GetOrganizationId(), req.GetPageSize(), req.GetPageToken())
	if err != nil {
		return nil, toStatus(err)
	}
	out := &organizationv1.ListMembersResponse{
		NextPageToken: res.NextToken,
		TotalSize:     res.TotalSize,
	}
	for _, m := range res.Members {
		out.Members = append(out.Members, toMemberAPI(m))
	}
	return out, nil
}

// Leave 处理成员自行离开（最后一个 owner 拒绝）。
func (s *Service) Leave(ctx context.Context, req *organizationv1.LeaveRequest) (*emptypb.Empty, error) {
	who, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.uc.Leave(ctx, who, req.GetOrganizationId()); err != nil {
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
	case errors.Is(err, organization.ErrOrganizationNotFound):
		return status.New(codes.NotFound, err.Error()).Err()
	case errors.Is(err, organization.ErrNotMember):
		return status.New(codes.PermissionDenied, err.Error()).Err()
	case errors.Is(err, organization.ErrNotOwner):
		return status.New(codes.PermissionDenied, err.Error()).Err()
	case errors.Is(err, organization.ErrSlugTaken):
		return status.New(codes.AlreadyExists, err.Error()).Err()
	case errors.Is(err, organization.ErrInvalidSlug),
		errors.Is(err, organization.ErrEmptyName),
		errors.Is(err, organization.ErrUnknownUser):
		return status.New(codes.InvalidArgument, err.Error()).Err()
	case errors.Is(err, organization.ErrCannotRemoveOwner),
		errors.Is(err, organization.ErrLastOwner):
		return status.New(codes.FailedPrecondition, err.Error()).Err()
	default:
		return err
	}
}

func toAPI(o *organization.Organization) *organizationv1.Organization {
	return &organizationv1.Organization{
		Id:        o.ID,
		Name:      o.Name,
		Slug:      o.Slug,
		CreatedAt: timestamppb.New(o.CreatedAt),
		UpdatedAt: timestamppb.New(o.UpdatedAt),
	}
}

func toMemberAPI(m *organization.Member) *organizationv1.OrganizationMember {
	return &organizationv1.OrganizationMember{
		UserId:   m.UserID,
		IsOwner:  m.IsOwner,
		JoinedAt: timestamppb.New(m.JoinedAt),
	}
}
