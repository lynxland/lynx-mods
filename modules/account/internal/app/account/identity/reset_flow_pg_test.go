package identity

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/lynxland/appkit/clock"
	"github.com/lynxland/appkit/contrib/bunx/testutil"
	"github.com/lynxland/appkit/jwt"
	"github.com/lynxland/appkit/uow"
	"github.com/lynxland/lynx-start/modules/account/domain/account/identity"
	infraindentity "github.com/lynxland/lynx-start/modules/account/internal/infra/account/identity"
	"github.com/lynxland/lynx-start/modules/account/internal/infra/account/migrations"
)

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

func TestResetReproPG(t *testing.T) {
	testutil.SkipUnlessPg(t)
	db := testutil.NewDB(t, testutil.Chain{Module: "account", FS: migrations.FS})
	repo := infraindentity.NewRepository(db)
	fake := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	signer := jwt.NewSigner("m", PurposeAccessToken, "iss", 30*time.Minute, fake)
	verifier := jwt.NewVerifier("m", PurposeAccessToken, "iss")
	oneShot := jwt.NewSigner("m", "identity-one-shot", "iss", OneShotTokenTTL, fake)
	oneShotV := jwt.NewVerifier("m", "identity-one-shot", "iss")
	pub := testutil.NewCapturePublisher()
	uc := New(repo, uow.Runner(testutil.InlineRunner{}), pub, signer, verifier, oneShot, oneShotV, nil, fake)
	ctx := context.Background()

	if _, _, err := uc.Register(ctx, "rp@x.test", "password123", "RP"); err != nil {
		t.Fatal(err)
	}
	if err := uc.RequestPasswordReset(ctx, "rp@x.test"); err != nil {
		t.Fatal(err)
	}
	var token string
	for _, e := range pub.Envelopes() {
		if e.Name == identity.EventUserResetRequested {
			// 载荷字段名是 token(小写)
			var p map[string]string
			_ = jsonUnmarshal(e.Payload, &p)
			token = p["token"]
		}
	}
	if token == "" {
		t.Fatal("no token in event")
	}
	if err := uc.ConfirmPasswordReset(ctx, token, "newpass-456"); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	u, _, err := uc.Login(ctx, "rp@x.test", "newpass-456")
	if err != nil {
		t.Fatalf("login after reset: %v", err)
	}
	_ = u
}
