package session

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/lynxland/appkit/clock"
	"github.com/lynxland/appkit/events"
	"github.com/lynxland/appkit/jwt"
	"github.com/lynxland/lynx-mods/modules/session/domain/session"
	appsession "github.com/lynxland/lynx-mods/modules/session/internal/app/session"
	"github.com/lynxland/lynx/eventbus"
)

// TestPasswordChangedWatcher 跨模块联动集成：identity 的改密事件（经
// 总线 topic module.identity 投递）→ session 全局吊销。内存总线异步
// 投递，轮询等待生效。
func TestPasswordChangedWatcher(t *testing.T) {
	bus := eventbus.NewMemoryBus(eventbus.Options{})
	pub := events.NewBusPublisher(bus)

	fake := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	tokens := appsession.Tokens{
		Access:        jwt.NewSigner("m", appsession.PurposeAccessToken, "iss", 15*time.Minute, fake),
		Refresh:       jwt.NewSigner("m", appsession.PurposeRefreshToken, "iss", time.Hour, fake),
		AccessVerify:  jwt.NewVerifier("m", appsession.PurposeAccessToken, "iss"),
		RefreshVerify: jwt.NewVerifier("m", appsession.PurposeRefreshToken, "iss"),
		RefreshTTL:    time.Hour,
	}
	srepo := newWatcherRepo()
	uc := appsession.New(srepo, nil, watcherTx{}, nilPub{}, tokens, fake)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := NewPasswordChangedWatcher(bus, uc, slog.Default())
	go func() { _ = w.Start(ctx) }()

	// 等订阅生效。
	time.Sleep(100 * time.Millisecond)

	s := mustSession(t, "s1", "u1")
	_ = srepo.Create(ctx, s)

	// 发布 identity.user.password-changed（模拟 identity 侧事件目录）。
	payload, _ := json.Marshal(map[string]string{"id": "u1"})
	if err := pub.Publish(ctx, events.Envelope{
		ID: "evt-1", Name: "identity.user.password-changed", Module: "identity", Payload: payload,
	}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		got, err := srepo.Get(ctx, "s1")
		if err == nil && got.Status == session.StatusRevoked && got.RevokeReason == session.RevokePassword {
			return // 联动生效
		}
		if time.Now().After(deadline) {
			t.Fatalf("watcher 未生效: got=%+v err=%v", got, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestPasswordChangedWatcherIgnoresOthers 同 topic 其他事件不触发吊销。
func TestPasswordChangedWatcherIgnoresOthers(t *testing.T) {
	bus := eventbus.NewMemoryBus(eventbus.Options{})
	pub := events.NewBusPublisher(bus)
	fake := clock.NewFake(time.Now())
	tokens := appsession.Tokens{
		Access:     jwt.NewSigner("m", appsession.PurposeAccessToken, "iss", time.Minute, fake),
		Refresh:    jwt.NewSigner("m", appsession.PurposeRefreshToken, "iss", time.Hour, fake),
		RefreshTTL: time.Hour,
	}
	srepo := newWatcherRepo()
	uc := appsession.New(srepo, nil, watcherTx{}, nilPub{}, tokens, fake)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = NewPasswordChangedWatcher(bus, uc, slog.Default()).Start(ctx) }()
	time.Sleep(100 * time.Millisecond)

	_ = srepo.Create(ctx, mustSession(t, "s1", "u1"))
	_ = pub.Publish(ctx, events.Envelope{
		ID: "evt-2", Name: "identity.user.created", Module: "identity",
		Payload: []byte(`{"id":"u1"}`),
	})
	time.Sleep(300 * time.Millisecond)
	got, _ := srepo.Get(ctx, "s1")
	if got.Status != session.StatusActive {
		t.Fatalf("identity.user.created 不应触发吊销: %+v", got)
	}
}

func mustSession(t *testing.T, id, userID string) *session.Session {
	t.Helper()
	s, err := session.NewSession(id, userID, "jti", time.Now().UTC(), time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// watcherRepo：只覆盖 watcher 路径所需的最小仓储实现（轮询读 + 吊销计数）。
type watcherRepo struct {
	mu   sync.Mutex
	rows map[string]*session.Session
}

func newWatcherRepo() *watcherRepo { return &watcherRepo{rows: map[string]*session.Session{}} }

func (r *watcherRepo) Create(_ context.Context, s *session.Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := *s
	r.rows[s.ID] = &c
	return nil
}

func (r *watcherRepo) Get(_ context.Context, id string) (*session.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.rows[id]; ok {
		c := *s
		return &c, nil
	}
	return nil, session.ErrSessionNotFound
}

func (r *watcherRepo) Rotate(_ context.Context, s *session.Session) error { return nil }

func (r *watcherRepo) Revoke(_ context.Context, id, reason string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.rows[id]; ok {
		s.Revoke(reason, at)
	}
	return nil
}

func (r *watcherRepo) RevokeByUser(_ context.Context, userID, reason string, at time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, s := range r.rows {
		if s.UserID == userID && s.Status == session.StatusActive {
			s.Revoke(reason, at)
			n++
		}
	}
	return n, nil
}

type watcherTx struct{}

func (watcherTx) Run(_ context.Context, fn func(ctx context.Context) error) error {
	return fn(context.Background())
}

type nilPub struct{}

func (nilPub) Publish(context.Context, events.Envelope) error { return nil }
