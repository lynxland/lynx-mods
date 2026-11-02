package organization

import (
	"context"
	"testing"

	grpcapipolicy "github.com/lynxland/grpcapi/policy"
	grpcapiprotopolicy "github.com/lynxland/grpcapi/policy/protopolicy"
)

// TestPolicyBuildable 契约护栏：proto 注解必须能通过 grpcapi 的策略构建
// （fail-closed 启动断言的仓库内前置），并断言匿名面收敛（v1 全部方法
// 要求认证——成员资格/owner 校验在 app 层，不做组织级 RBAC）。
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
	methods := []string{
		"/organization.v1.OrganizationService/CreateOrganization",
		"/organization.v1.OrganizationService/GetOrganization",
		"/organization.v1.OrganizationService/ListMyOrganizations",
		"/organization.v1.OrganizationService/RenameOrganization",
		"/organization.v1.OrganizationService/AddMember",
		"/organization.v1.OrganizationService/RemoveMember",
		"/organization.v1.OrganizationService/ListMembers",
		"/organization.v1.OrganizationService/Leave",
	}
	for _, m := range methods {
		if _, ok := set.Get(m); !ok {
			t.Errorf("policy set missing %s", m)
		}
		if set.IsAnonymous(m) {
			t.Errorf("%s: anonymous = true, want false（全部方法要求认证）", m)
		}
	}
}
