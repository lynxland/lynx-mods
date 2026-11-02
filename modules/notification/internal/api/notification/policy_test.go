package notification

import (
	"context"
	"testing"

	grpcapipolicy "github.com/lynxland/grpcapi/policy"
	grpcapiprotopolicy "github.com/lynxland/grpcapi/policy/protopolicy"
)

// TestPolicyBuildable 契约护栏：proto 注解必须能通过 grpcapi 的策略构建
// （fail-closed 启动断言的仓库内前置），并断言匿名面收敛（v1 全部方法
// 要求认证——通知只可见于收件人本人，无需 permission）。
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
		"/notification.v1.NotificationService/ListNotifications",
		"/notification.v1.NotificationService/MarkNotificationRead",
		"/notification.v1.NotificationService/MarkAllNotificationsRead",
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
