// Package session 是会话模块的 API 层：gRPC 处理器、Bearer 认证器与
// 装配入口（Module/Assemble）。
package session

import (
	"context"
	"errors"

	"github.com/lynxland/lynx-mods/modules/session/domain/session"
	sessionv1 "github.com/lynxland/lynx-mods/modules/session/genproto/session/v1"
	appsession "github.com/lynxland/lynx-mods/modules/session/internal/app/session"

	"github.com/lynxland/grpcapi/principal"
	"github.com/lynxland/lynx-mods/modules/identity/domain/identity"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/lynxland/appkit/errcode"
)

// Service 实现 SessionService（薄转发：业务在用例层）。
type Service struct {
	sessionv1.UnimplementedSessionServiceServer
	uc *appsession.UseCase
}

// NewService 构造处理器。
func NewService(uc *appsession.UseCase) *Service { return &Service{uc: uc} }

// CreateSession 处理「已认证主体升级为可吊销会话」。
func (s *Service) CreateSession(ctx context.Context, _ *sessionv1.CreateSessionRequest) (*sessionv1.CreateSessionResponse, error) {
	p, ok := principal.From(ctx)
	if !ok || p.ID == "" {
		return nil, status.Error(codes.Unauthenticated, "authentication required")
	}
	pair, err := s.uc.CreateSession(ctx, p.ID, p.Attrs["email"])
	if err != nil {
		return nil, toStatus(err)
	}
	return toCreateResponse(pair), nil
}

// Refresh 处理刷新令牌轮换。
func (s *Service) Refresh(ctx context.Context, req *sessionv1.RefreshRequest) (*sessionv1.RefreshResponse, error) {
	pair, err := s.uc.Refresh(ctx, req.GetRefreshToken())
	if err != nil {
		return nil, toStatus(err)
	}
	out := toCreateResponse(pair)
	return &sessionv1.RefreshResponse{
		SessionId: out.SessionId, UserId: out.UserId,
		AccessToken: out.AccessToken, RefreshToken: out.RefreshToken,
		TokenType: out.TokenType, AccessExpiresAt: out.AccessExpiresAt,
		SessionExpiresAt: out.SessionExpiresAt,
	}, nil
}

// Logout 处理登出当前会话（principal 须携带 sid——即本模块签发的令牌）。
func (s *Service) Logout(ctx context.Context, _ *sessionv1.LogoutRequest) (*emptypb.Empty, error) {
	p, ok := principal.From(ctx)
	if !ok || p.ID == "" {
		return nil, status.Error(codes.Unauthenticated, "authentication required")
	}
	sid := p.Attrs["sid"]
	if sid == "" {
		return nil, status.Error(codes.InvalidArgument, "current credential is not a session token")
	}
	if err := s.uc.Logout(ctx, sid); err != nil {
		return nil, toStatus(err)
	}
	return &emptypb.Empty{}, nil
}

// LogoutAll 处理全端下线（任意认证主体可调——identity 令牌也算数）。
func (s *Service) LogoutAll(ctx context.Context, _ *sessionv1.LogoutAllRequest) (*sessionv1.LogoutAllResponse, error) {
	p, ok := principal.From(ctx)
	if !ok || p.ID == "" {
		return nil, status.Error(codes.Unauthenticated, "authentication required")
	}
	n, err := s.uc.LogoutAll(ctx, p.ID)
	if err != nil {
		return nil, toStatus(err)
	}
	return &sessionv1.LogoutAllResponse{RevokedCount: int32(n)}, nil
}

// toStatus 把区段哨兵错误（session 与依赖方 identity 两区段）映射为
// 携带 ErrorInfo 的 grpc status；非区段错误原样透传。
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
	case session.ErrSessionNotFound:
		code = codes.NotFound
	case session.ErrSessionRevoked,
		session.ErrSessionExpired,
		session.ErrRefreshRejected,
		identity.ErrInvalidCredentials:
		code = codes.Unauthenticated
	case identity.ErrUserDisabled:
		code = codes.PermissionDenied
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

func toCreateResponse(pair *appsession.TokenPair) *sessionv1.CreateSessionResponse {
	return &sessionv1.CreateSessionResponse{
		SessionId:        pair.Session.ID,
		UserId:           pair.Session.UserID,
		AccessToken:      pair.Access.Value,
		RefreshToken:     pair.Refresh.Value,
		TokenType:        "Bearer",
		AccessExpiresAt:  timestamppb.New(pair.Access.ExpiresAt),
		SessionExpiresAt: timestamppb.New(pair.Session.ExpiresAt),
	}
}
