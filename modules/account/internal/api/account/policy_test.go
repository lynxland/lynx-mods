package account

import (
	"context"
	"testing"

	grpcapipolicy "github.com/lynxland/grpcapi/policy"
	grpcapiprotopolicy "github.com/lynxland/grpcapi/policy/protopolicy"
)

// TestPolicyBuildable 契约护栏（ADR-0009 合并后覆盖三个域的 proto）：注解
// 必须能通过 grpcapi 的策略构建（fail-closed 启动断言的仓库内前置——每个
// 方法要么显式匿名、要么至少一条 requirement，漏声明在这里就红，不等应用
// 启动才炸）。同时断言匿名面收敛：identity 仅 Register/Login、session 仅
// Refresh、access 全部要求认证。
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
		// identity 域
		"/identity.v1.IdentityService/Register",
		"/identity.v1.IdentityService/Login",
		"/identity.v1.IdentityService/GetMe",
		"/identity.v1.IdentityService/ChangePassword",
		// session 域
		"/session.v1.SessionService/CreateSession",
		"/session.v1.SessionService/Refresh",
		"/session.v1.SessionService/Logout",
		"/session.v1.SessionService/LogoutAll",
		// access 域
		"/access.v1.AccessService/GrantRole",
		"/access.v1.AccessService/RevokeRole",
		"/access.v1.AccessService/ListUserRoles",
		"/access.v1.AccessService/ListMyRoles",
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
		"/session.v1.SessionService/CreateSession":    false,
		"/session.v1.SessionService/Refresh":          true,
		"/session.v1.SessionService/Logout":           false,
		"/session.v1.SessionService/LogoutAll":        false,
		"/access.v1.AccessService/GrantRole":          false,
		"/access.v1.AccessService/RevokeRole":         false,
		"/access.v1.AccessService/ListUserRoles":      false,
		"/access.v1.AccessService/ListMyRoles":        false,
	}
	for m, want := range anonymous {
		if got := set.IsAnonymous(m); got != want {
			t.Errorf("%s: anonymous = %v, want %v", m, got, want)
		}
	}
}
