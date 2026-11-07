// Package task 是后台任务与调度模块的 API 层：gRPC/gateway 服务、错误
// 投影与装配（Module/Assemble）。worker runner 的构造器转发导出
// （NewRunner）——应用组合根携带 handler 表接线（见模块 README）。
package task

import (
	"context"
	"errors"
	"time"

	"github.com/lynxland/lynx-start/modules/task/domain/task"
	taskv1 "github.com/lynxland/lynx-start/modules/task/genproto/task/v1"
	apptask "github.com/lynxland/lynx-start/modules/task/internal/app/task"

	"github.com/lynxland/appkit/errcode"
	"github.com/lynxland/appkit/errstatus"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Service 实现 taskv1.TaskServiceServer。
type Service struct {
	uc *apptask.UseCase
	taskv1.UnimplementedTaskServiceServer
}

// NewService 构造服务（Assemble 调用）。
func NewService(uc *apptask.UseCase) *Service { return &Service{uc: uc} }

var reasonCode = map[string]codes.Code{
	task.ErrJobNotFound.Reason:   codes.NotFound,
	task.ErrRuleNotFound.Reason:  codes.NotFound,
	task.ErrInvalidCron.Reason:   codes.InvalidArgument,
	task.ErrInvalidJob.Reason:    codes.InvalidArgument,
	task.ErrNotPending.Reason:    codes.FailedPrecondition,
	task.ErrNotRetryable.Reason:  codes.FailedPrecondition,
	task.ErrDuplicateType.Reason: codes.AlreadyExists,
}

func toStatus(err error) error {
	var info errcode.Info
	if !errors.As(err, &info) {
		return errstatus.Project(err, errstatus.Options{})
	}
	return errstatus.Project(info, errstatus.Options{CodeMap: reasonCode})
}

// Enqueue 入队（即时/延迟/定时；唯一键防重）。
func (s *Service) Enqueue(ctx context.Context, req *taskv1.EnqueueRequest) (*taskv1.Job, error) {
	in := apptask.EnqueueInput{
		Type:        req.GetType(),
		Payload:     req.GetPayload(),
		UniqueKey:   req.GetUniqueKey(),
		Priority:    req.GetPriority(),
		MaxAttempts: req.GetMaxAttempts(),
	}
	if req.GetRunAt() != nil {
		t := req.GetRunAt().AsTime()
		in.RunAt = &t
	}
	if req.GetDelaySeconds() > 0 {
		d := time.Duration(req.GetDelaySeconds()) * time.Second
		in.Delay = &d
	}
	j, _, err := s.uc.Enqueue(ctx, in)
	if err != nil {
		return nil, toStatus(err)
	}
	return toAPIJob(j), nil
}

// GetJob 取任务。
func (s *Service) GetJob(ctx context.Context, req *taskv1.GetJobRequest) (*taskv1.Job, error) {
	j, err := s.uc.Get(ctx, req.GetId())
	if err != nil {
		return nil, toStatus(err)
	}
	return toAPIJob(j), nil
}

// ListJobs 列任务（过滤+keyset 分页）。
func (s *Service) ListJobs(ctx context.Context, req *taskv1.ListJobsRequest) (*taskv1.ListJobsResponse, error) {
	res, err := s.uc.List(ctx, task.JobFilter{Status: toDomainStatus(req.GetStatus()), Type: req.GetType()},
		req.GetPageSize(), req.GetPageToken())
	if err != nil {
		return nil, toStatus(err)
	}
	resp := &taskv1.ListJobsResponse{
		Jobs:          make([]*taskv1.Job, 0, len(res.Jobs)),
		NextPageToken: res.NextToken,
		TotalSize:     res.TotalSize,
	}
	for _, j := range res.Jobs {
		resp.Jobs = append(resp.Jobs, toAPIJob(j))
	}
	return resp, nil
}

// RetryJob 重试死信/已取消。
func (s *Service) RetryJob(ctx context.Context, req *taskv1.RetryJobRequest) (*taskv1.Job, error) {
	j, err := s.uc.Retry(ctx, req.GetId())
	if err != nil {
		return nil, toStatus(err)
	}
	return toAPIJob(j), nil
}

// CancelJob 取消待执行任务。
func (s *Service) CancelJob(ctx context.Context, req *taskv1.CancelJobRequest) (*taskv1.Job, error) {
	j, err := s.uc.Cancel(ctx, req.GetId())
	if err != nil {
		return nil, toStatus(err)
	}
	return toAPIJob(j), nil
}

// GetStats 各状态计数。
func (s *Service) GetStats(ctx context.Context, _ *taskv1.GetStatsRequest) (*taskv1.Stats, error) {
	st, err := s.uc.Stats(ctx)
	if err != nil {
		return nil, toStatus(err)
	}
	return &taskv1.Stats{
		Pending: st.Pending, Running: st.Running, Done: st.Done,
		Dead: st.Dead, Cancelled: st.Cancelled,
	}, nil
}

// ListRecurringRules 列周期规则。
func (s *Service) ListRecurringRules(ctx context.Context, req *taskv1.ListRecurringRulesRequest) (*taskv1.ListRecurringRulesResponse, error) {
	res, err := s.uc.ListRules(ctx, req.GetPageSize(), req.GetPageToken())
	if err != nil {
		return nil, toStatus(err)
	}
	resp := &taskv1.ListRecurringRulesResponse{
		Rules:         make([]*taskv1.RecurringRule, 0, len(res.Rules)),
		NextPageToken: res.NextToken,
		TotalSize:     res.TotalSize,
	}
	for _, r := range res.Rules {
		resp.Rules = append(resp.Rules, toAPIRule(r))
	}
	return resp, nil
}

// UpsertRecurringRule 新建/更新周期规则。
func (s *Service) UpsertRecurringRule(ctx context.Context, req *taskv1.UpsertRecurringRuleRequest) (*taskv1.RecurringRule, error) {
	r, err := s.uc.UpsertRule(ctx, req.GetId(), req.GetType(), req.GetPayload(), req.GetCron(), req.GetEnabled())
	if err != nil {
		return nil, toStatus(err)
	}
	return toAPIRule(r), nil
}

// DeleteRecurringRule 删除周期规则。
func (s *Service) DeleteRecurringRule(ctx context.Context, req *taskv1.DeleteRecurringRuleRequest) (*emptypb.Empty, error) {
	if err := s.uc.DeleteRule(ctx, req.GetId()); err != nil {
		return nil, toStatus(err)
	}
	return &emptypb.Empty{}, nil
}

// toDomainStatus 把 proto 枚举映射为域状态（UNSPECIFIED → 空 = 不过滤）。
func toDomainStatus(st taskv1.Status) task.Status {
	switch st {
	case taskv1.Status_STATUS_PENDING:
		return task.StatusPending
	case taskv1.Status_STATUS_RUNNING:
		return task.StatusRunning
	case taskv1.Status_STATUS_DONE:
		return task.StatusDone
	case taskv1.Status_STATUS_DEAD:
		return task.StatusDead
	case taskv1.Status_STATUS_CANCELLED:
		return task.StatusCancelled
	default:
		return ""
	}
}

func toAPIStatus(st task.Status) taskv1.Status {
	switch st {
	case task.StatusPending:
		return taskv1.Status_STATUS_PENDING
	case task.StatusRunning:
		return taskv1.Status_STATUS_RUNNING
	case task.StatusDone:
		return taskv1.Status_STATUS_DONE
	case task.StatusDead:
		return taskv1.Status_STATUS_DEAD
	case task.StatusCancelled:
		return taskv1.Status_STATUS_CANCELLED
	default:
		return taskv1.Status_STATUS_UNSPECIFIED
	}
}

func toAPIJob(j *task.Job) *taskv1.Job {
	out := &taskv1.Job{
		Id: j.ID, Type: j.Type, Payload: j.Payload,
		Status: toAPIStatus(j.Status), Priority: j.Priority,
		Attempt: j.Attempt, MaxAttempts: j.MaxAttempts,
		UniqueKey: j.UniqueKey, LastError: j.LastError,
		RunAt:     timestamppb.New(j.RunAt),
		CreatedAt: timestamppb.New(j.CreatedAt),
	}
	if j.FinishedAt != nil {
		out.FinishedAt = timestamppb.New(*j.FinishedAt)
	}
	return out
}

func toAPIRule(r *task.RecurringRule) *taskv1.RecurringRule {
	out := &taskv1.RecurringRule{
		Id: r.ID, Type: r.Type, Payload: r.Payload, Cron: r.CronExpr,
		Enabled:   r.Enabled,
		NextRunAt: timestamppb.New(r.NextRunAt),
		CreatedAt: timestamppb.New(r.CreatedAt),
		UpdatedAt: timestamppb.New(r.UpdatedAt),
	}
	if r.LastRunAt != nil {
		out.LastRunAt = timestamppb.New(*r.LastRunAt)
	}
	return out
}
