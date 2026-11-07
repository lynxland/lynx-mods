package task

import (
	"context"
	"testing"

	grpcapipolicy "github.com/lynxland/grpcapi/policy"
	grpcapiprotopolicy "github.com/lynxland/grpcapi/policy/protopolicy"
)

// TestPolicyBuildable 契约护栏：策略可构建 + 权限面收敛（全部要求认证；
// 管理面按 task.job.read / task.job.write / task.rule.write 裁决——claims
// 由 account 的 access 域注入）。
func TestPolicyBuildable(t *testing.T) {
	mod := Module()
	set, err := grpcapipolicy.Build(context.Background(),
		grpcapipolicy.FromFiles(mod.Policies...),
		grpcapipolicy.Options{
			Provider: grpcapiprotopolicy.New(grpcapiprotopolicy.Options{}),
			Registry: grpcapipolicy.DefaultRegistry(),
		},
	)
	if err != nil {
		t.Fatalf("policy build: %v", err)
	}
	for _, m := range []string{
		"/task.v1.TaskService/Enqueue",
		"/task.v1.TaskService/GetJob",
		"/task.v1.TaskService/ListJobs",
		"/task.v1.TaskService/RetryJob",
		"/task.v1.TaskService/CancelJob",
		"/task.v1.TaskService/GetStats",
		"/task.v1.TaskService/ListRecurringRules",
		"/task.v1.TaskService/UpsertRecurringRule",
		"/task.v1.TaskService/DeleteRecurringRule",
	} {
		if _, ok := set.Get(m); !ok {
			t.Errorf("policy set missing %s", m)
		}
		if set.IsAnonymous(m) {
			t.Errorf("%s must not be anonymous", m)
		}
	}
}
