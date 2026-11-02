package audit

import (
	"context"
	"testing"

	grpcapipolicy "github.com/lynxland/grpcapi/policy"
	grpcapiprotopolicy "github.com/lynxland/grpcapi/policy/protopolicy"
)

// TestPolicyBuildable 契约护栏：proto 注解必须能通过 grpcapi 的策略构建
// （fail-closed 启动断言的仓库内前置）。同时断言匿名面收敛：审计模块
// 没有任何匿名方法（记录与查询都要主体）。
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
		"/audit.v1.AuditService/Record",
		"/audit.v1.AuditService/ListAuditRecords",
	} {
		if _, ok := set.Get(m); !ok {
			t.Errorf("policy set missing %s", m)
		}
		if set.IsAnonymous(m) {
			t.Errorf("%s must not be anonymous", m)
		}
	}
}
