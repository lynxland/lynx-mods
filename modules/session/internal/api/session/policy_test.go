package session

import (
	"context"
	"testing"

	grpcapipolicy "github.com/lynxland/grpcapi/policy"
	grpcapiprotopolicy "github.com/lynxland/grpcapi/policy/protopolicy"
)

// TestPolicyBuildable 契约护栏：proto 注解必须能通过 grpcapi 的策略构建
// （fail-closed 启动断言的仓库内前置），并断言匿名面收敛（仅 Refresh）。
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
		"/session.v1.SessionService/CreateSession",
		"/session.v1.SessionService/Refresh",
		"/session.v1.SessionService/Logout",
		"/session.v1.SessionService/LogoutAll",
	} {
		if _, ok := set.Get(m); !ok {
			t.Errorf("policy set missing %s", m)
		}
	}
	anonymous := map[string]bool{
		"/session.v1.SessionService/CreateSession": false,
		"/session.v1.SessionService/Refresh":       true,
		"/session.v1.SessionService/Logout":        false,
		"/session.v1.SessionService/LogoutAll":     false,
	}
	for m, want := range anonymous {
		if got := set.IsAnonymous(m); got != want {
			t.Errorf("%s: anonymous = %v, want %v", m, got, want)
		}
	}
}
