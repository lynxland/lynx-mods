package audit

import (
	"context"
	"encoding/json"
	"errors"

	auditv1 "github.com/lynx-go/lynx-mods/modules/audit/genproto/audit/v1"
	appaudit "github.com/lynx-go/lynx-mods/modules/audit/internal/app/audit"

	domainaudit "github.com/lynx-go/lynx-mods/modules/audit/domain/audit"
	"github.com/lynxland/appkit/contexts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Service 实现 AuditService（薄转发：actor 注入在 API 层，业务在用例层）。
type Service struct {
	auditv1.UnimplementedAuditServiceServer
	uc *appaudit.UseCase
}

// NewService 构造处理器。
func NewService(uc *appaudit.UseCase) *Service { return &Service{uc: uc} }

// Record 处理显式记录：actor 取自认证主体（policy 已要求 authenticated，
// 不可伪造他人）；payload 是业务自定义 JSON 文本。
func (s *Service) Record(ctx context.Context, req *auditv1.RecordRequest) (*auditv1.AuditRecord, error) {
	actorID := contexts.SubjectID(ctx)
	var payload json.RawMessage
	if p := req.GetPayload(); p != "" {
		payload = json.RawMessage(p)
	}
	rec, err := s.uc.Record(ctx, actorID, actorEmail(ctx), req.GetAction(), req.GetResourceType(), req.GetResourceId(), payload)
	if err != nil {
		return nil, toStatus(err)
	}
	return toAPI(rec), nil
}

// ListAuditRecords 处理查询（occurred_at 降序、结构化过滤、分页）。
func (s *Service) ListAuditRecords(ctx context.Context, req *auditv1.ListAuditRecordsRequest) (*auditv1.ListAuditRecordsResponse, error) {
	f := domainaudit.Filter{
		ActorID:      req.GetActorId(),
		Action:       req.GetAction(),
		ResourceType: req.GetResourceType(),
		ResourceID:   req.GetResourceId(),
	}
	recs, page, err := s.uc.ListRecords(ctx, f, req.GetPageSize(), req.GetPageToken())
	if err != nil {
		return nil, toStatus(err)
	}
	resp := &auditv1.ListAuditRecordsResponse{
		Records:       make([]*auditv1.AuditRecord, 0, len(recs)),
		TotalSize:     int32(page.TotalCount),
		NextPageToken: page.NextToken,
	}
	for _, rec := range recs {
		resp.Records = append(resp.Records, toAPI(rec))
	}
	return resp, nil
}

// actorEmail 从 Principal 的单值属性取 email（认证器注入；缺省空串）。
func actorEmail(ctx context.Context) string {
	p, ok := contexts.FromPrincipal(ctx)
	if !ok || p == nil {
		return ""
	}
	return p.Attrs["email"]
}

func toAPI(rec *domainaudit.Record) *auditv1.AuditRecord {
	return &auditv1.AuditRecord{
		Id:           rec.ID,
		ActorId:      rec.ActorID,
		ActorEmail:   rec.ActorEmail,
		Action:       rec.Action,
		ResourceType: rec.ResourceType,
		ResourceId:   rec.ResourceID,
		Payload:      string(rec.Payload),
		Source:       rec.Source,
		OccurredAt:   timestamppb.New(rec.OccurredAt),
	}
}

// toStatus 映射领域哨兵错误（errcode → InvalidArgument），其余原样上抛。
func toStatus(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, domainaudit.ErrEmptyAction), errors.Is(err, domainaudit.ErrPayloadTooLarge):
		return status.New(codes.InvalidArgument, err.Error()).Err()
	default:
		return err
	}
}
