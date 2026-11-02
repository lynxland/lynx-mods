package identity

import (
	"context"
	"testing"

	grpcapipolicy "github.com/lynxland/grpcapi/policy"
	grpcapiprotopolicy "github.com/lynxland/grpcapi/policy/protopolicy"
)

// TestPolicyBuildable 契约护栏：proto 注解必须能通过 grpcapi 的策略构建
// （fail-closed 启动断言的仓库内前置——每个方法要么显式匿名、要么至少
// 一条 requirement，漏声明在这里就红，不等应用启动才炸）。
// 同时断言匿名面收敛：只有 Register/Login 匿名可访问。
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
		"/identity.v1.IdentityService/Register",
		"/identity.v1.IdentityService/Login",
		"/identity.v1.IdentityService/GetMe",
		"/identity.v1.IdentityService/ChangePassword",
	} {
		if _, ok := set.Get(m); !ok {
			t.Errorf("policy set missing %s", m)
		}
	}
	anonymous := map[string]bool{
		"/identity.v1.IdentityService/Register":       true,
		"/identity.v1.IdentityService/Login":          true,
		"/identity.v1.IdentityService/GetMe":          false,
		"/identity.v1.IdentityService/ChangePassword": false,
	}
	for m, want := range anonymous {
		if got := set.IsAnonymous(m); got != want {
			t.Errorf("%s: anonymous = %v, want %v", m, got, want)
		}
	}
}
