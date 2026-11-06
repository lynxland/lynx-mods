package session

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func TestSessionLifecycle(t *testing.T) {
	s, err := NewSession("s1", "u1", "jti-1", t0, t0.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !s.IsActive(t0) {
		t.Error("fresh session should be active")
	}

	t.Run("rotate", func(t *testing.T) {
		later := t0.Add(time.Hour)
		if err := s.Rotate("jti-2", later); err != nil {
			t.Fatal(err)
		}
		if s.RefreshJTI != "jti-2" || !s.LastRefreshedAt.Equal(later) {
			t.Errorf("after rotate: %+v", s)
		}
		if s.Rotate("", later) == nil {
			t.Error("empty jti: want error")
		}
	})

	t.Run("expiry", func(t *testing.T) {
		if s.IsActive(t0.Add(25 * time.Hour)) {
			t.Error("session past ExpiresAt should be inactive")
		}
	})

	t.Run("revoke idempotent", func(t *testing.T) {
		at := t0.Add(2 * time.Hour)
		s.Revoke(RevokeLogout, at)
		if s.Status != StatusRevoked || s.RevokeReason != RevokeLogout || !s.RevokedAt.Equal(at) {
			t.Errorf("after revoke: %+v", s)
		}
		s.Revoke(RevokePassword, t0.Add(3*time.Hour))
		if !s.RevokedAt.Equal(at) || s.RevokeReason != RevokeLogout {
			t.Error("second revoke must not overwrite first revoke time/reason")
		}
		if s.IsActive(t0.Add(time.Minute)) {
			t.Error("revoked session should be inactive")
		}
	})
}

func TestNewSessionGuards(t *testing.T) {
	for _, args := range [][3]string{{"", "u", "j"}, {"s", "", "j"}, {"s", "u", ""}} {
		if _, err := NewSession(args[0], args[1], args[2], t0, t0); err == nil {
			t.Errorf("NewSession(%v): want error", args)
		}
	}
}
