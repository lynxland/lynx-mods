package identity

import (
	"errors"
	"testing"
	"time"
)

func TestNewUser(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("normalizes email and defaults display name", func(t *testing.T) {
		u, err := NewUser("u1", "  Alice@Example.COM ", "", "hash", now)
		if err != nil {
			t.Fatal(err)
		}
		if u.Email != "alice@example.com" {
			t.Errorf("email = %q, want alice@example.com", u.Email)
		}
		if u.DisplayName != "alice" {
			t.Errorf("display name = %q, want local part", u.DisplayName)
		}
		if u.Status != StatusActive {
			t.Errorf("status = %q, want active", u.Status)
		}
	})

	t.Run("rejects invalid email", func(t *testing.T) {
		for _, email := range []string{"", "no-at", "a@b", "a b@example.com", "a@b.c", "中文@example.com"} {
			if _, err := NewUser("u1", email, "", "hash", now); !errors.Is(err, ErrInvalidEmail) {
				t.Errorf("email %q: err = %v, want ErrInvalidEmail", email, err)
			}
		}
	})
}

func TestValidatePassword(t *testing.T) {
	cases := []struct {
		pw   string
		want error
	}{
		{"", ErrWeakPassword},
		{"1234567", ErrWeakPassword},
		{"12345678", nil},
		{string(make([]byte, MaxPasswordLength+1)), ErrWeakPassword},
	}
	for _, c := range cases {
		if err := ValidatePassword(c.pw); !errors.Is(err, c.want) {
			t.Errorf("len(%d): err = %v, want %v", len(c.pw), err, c.want)
		}
	}
}

func TestChangePassword(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	u, err := NewUser("u1", "a@example.com", "A", "old", now)
	if err != nil {
		t.Fatal(err)
	}
	later := now.Add(time.Hour)
	if err := u.ChangePassword("new", later); err != nil {
		t.Fatal(err)
	}
	if u.PasswordHash != "new" {
		t.Errorf("hash = %q, want new", u.PasswordHash)
	}
	if !u.UpdatedAt.Equal(later) {
		t.Errorf("updated_at = %v, want %v", u.UpdatedAt, later)
	}
	if err := u.ChangePassword("", later); err == nil {
		t.Error("empty hash: want error")
	}
}
