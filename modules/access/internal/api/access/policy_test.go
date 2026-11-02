package access

import (
	"context"
	"testing"

	grpcapipolicy "github.com/lynxland/grpcapi/policy"
	grpcapiprotopolicy "github.com/lynxland/grpcapi/policy/protopolicy"
)

// TestPolicyBuildable 契约护栏：策略可构建 + 匿名面收敛（全部要求认证；
// 管理面进一步要求 permissions，由 Enricher 注入的 claims 裁决）。
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
		"/access.v1.AccessService/GrantRole",
		"/access.v1.AccessService/RevokeRole",
		"/access.v1.AccessService/ListUserRoles",
		"/access.v1.AccessService/ListMyRoles",
	} {
		if _, ok := set.Get(m); !ok {
			t.Errorf("policy set missing %s", m)
		}
		if set.IsAnonymous(m) {
			t.Errorf("%s must not be anonymous", m)
		}
	}
}
