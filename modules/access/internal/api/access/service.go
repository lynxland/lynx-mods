// Package access 是授权模块的 API 层：gRPC 处理器、Principal 增强器与
// 装配入口（Module/Assemble）。
package access

import (
	"context"
	"errors"

	"github.com/lynxland/lynx-start/modules/access/domain/access"
	accessv1 "github.com/lynxland/lynx-start/modules/access/genproto/access/v1"
	appaccess "github.com/lynxland/lynx-start/modules/access/internal/app/access"

	"github.com/lynxland/grpcapi/principal"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/lynxland/appkit/errcode"
)

// Service 实现 AccessService（薄转发：业务在用例层）。
type Service struct {
	accessv1.UnimplementedAccessServiceServer
	uc *appaccess.UseCase
}

// NewService 构造处理器。
func NewService(uc *appaccess.UseCase) *Service { return &Service{uc: uc} }

// GrantRole 处理授予。
func (s *Service) GrantRole(ctx context.Context, req *accessv1.GrantRoleRequest) (*emptypb.Empty, error) {
	if err := s.uc.Grant(ctx, req.GetUserId(), req.GetRole()); err != nil {
		return nil, toStatus(err)
	}
	return &emptypb.Empty{}, nil
}

// RevokeRole 处理回收。
func (s *Service) RevokeRole(ctx context.Context, req *accessv1.RevokeRoleRequest) (*emptypb.Empty, error) {
	if err := s.uc.Revoke(ctx, req.GetUserId(), req.GetRole()); err != nil {
		return nil, toStatus(err)
	}
	return &emptypb.Empty{}, nil
}

// ListUserRoles 处理用户角色查询。
func (s *Service) ListUserRoles(ctx context.Context, req *accessv1.ListUserRolesRequest) (*accessv1.ListUserRolesResponse, error) {
	roles, err := s.uc.RolesForUser(ctx, req.GetUserId())
	if err != nil {
		return nil, toStatus(err)
	}
	return &accessv1.ListUserRolesResponse{Roles: roles}, nil
}

// ListMyRoles 处理自助角色/权限查询（权限即 Enricher 注入的 claims）。
func (s *Service) ListMyRoles(ctx context.Context, _ *accessv1.ListMyRolesRequest) (*accessv1.ListMyRolesResponse, error) {
	p, ok := principal.From(ctx)
	if !ok || p.ID == "" {
		return nil, status.Error(codes.Unauthenticated, "authentication required")
	}
	roles, err := s.uc.RolesForUser(ctx, p.ID)
	if err != nil {
		return nil, toStatus(err)
	}
	perms, err := s.uc.PermissionsForUser(ctx, p.ID)
	if err != nil {
		return nil, toStatus(err)
	}
	return &accessv1.ListMyRolesResponse{Roles: roles, Permissions: perms}, nil
}

// toStatus 把区段哨兵错误映射为携带 ErrorInfo 的 grpc status；
// 非区段错误原样透传。
func toStatus(err error) error {
	if err == nil {
		return nil
	}
	var info errcode.Info
	if !errors.As(err, &info) {
		return err
	}
	var code codes.Code
	switch info {
	case access.ErrUnknownRole:
		code = codes.InvalidArgument
	case access.ErrUnknownUser:
		code = codes.NotFound
	default:
		code = codes.Internal
	}
	st := status.New(code, info.Error())
	withDetails, derr := st.WithDetails(&errdetails.ErrorInfo{
		Domain: info.Domain,
		Reason: info.Reason,
	})
	if derr != nil {
		return st.Err()
	}
	return withDetails.Err()
}
