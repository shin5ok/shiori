package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
)

func linkStore(t *testing.T) *Store {
	t.Helper()
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("requires Firestore Emulator; run make test-integration")
	}
	client, err := firestore.NewClient(context.Background(), "demo-bookmark-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	return New(client)
}

func invite(t *testing.T, s *Store, base, email string) string {
	t.Helper()
	token := RandomToken()
	if err := s.CreateInvite(context.Background(), base, email, token, time.Hour); err != nil {
		t.Fatalf("invite %s: %v", email, err)
	}
	return token
}

func TestAccountLinkLifecycle(t *testing.T) {
	s := linkStore(t)
	ctx := context.Background()
	base, guest := RandomToken(), RandomToken()
	token := invite(t, s, base, "Guest@Example.com")
	if _, err := s.ConfirmInvite(ctx, token, guest, "other@example.com"); !errors.Is(err, ErrEmailMismatch) {
		t.Fatalf("email mismatch: %v", err)
	}
	if _, err := s.ConfirmInvite(ctx, token, base, "guest@example.com"); !errors.Is(err, ErrLinkRejected) {
		t.Fatalf("self link: %v", err)
	}
	link, err := s.ConfirmInvite(ctx, token, guest, "GUEST@example.com")
	if err != nil || link.BaseID != base {
		t.Fatalf("confirm: %#v %v", link, err)
	}
	if _, err = s.ConfirmInvite(ctx, token, guest, "guest@example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("invite reuse: %v", err)
	}
	got, err := s.Link(ctx, guest)
	if err != nil || got.BaseID != base {
		t.Fatalf("link: %#v %v", got, err)
	}
	if ok, err := s.HasLinkedEmail(ctx, base, "guest@example.com"); err != nil || !ok {
		t.Fatalf("linked email: %v %v", ok, err)
	}
	if _, err = s.Unlink(ctx, RandomToken(), guest); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unlink by other base: %v", err)
	}
	if _, err = s.Unlink(ctx, base, guest); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Link(ctx, guest); !errors.Is(err, ErrNotFound) {
		t.Fatalf("link after unlink: %v", err)
	}
}

func TestAccountLinkRules(t *testing.T) {
	s := linkStore(t)
	ctx := context.Background()
	base := RandomToken()
	var logins []string
	for i := 0; i < MaxLinkedAccounts; i++ {
		login := RandomToken()
		logins = append(logins, login)
		email := string(rune('a'+i)) + "@example.com"
		if _, err := s.ConfirmInvite(ctx, invite(t, s, base, email), login, email); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.CreateInvite(ctx, base, "extra@example.com", RandomToken(), time.Hour); !errors.Is(err, ErrLinkLimit) {
		t.Fatalf("sixth account: %v", err)
	}
	if err := s.CreateInvite(ctx, base, "a@example.com", RandomToken(), time.Hour); !errors.Is(err, ErrLinkRejected) {
		t.Fatalf("duplicate email: %v", err)
	}
	// A linked account cannot become a base, and a base cannot be linked (no chains).
	if err := s.CreateInvite(ctx, logins[0], "x@example.com", RandomToken(), time.Hour); !errors.Is(err, ErrLinkRejected) {
		t.Fatalf("linked account as base: %v", err)
	}
	other := RandomToken()
	token := invite(t, s, other, "base@example.com")
	if _, err := s.ConfirmInvite(ctx, token, base, "base@example.com"); !errors.Is(err, ErrLinkRejected) {
		t.Fatalf("base linked as guest: %v", err)
	}
	if _, err := s.ConfirmInvite(ctx, token, logins[1], "base@example.com"); !errors.Is(err, ErrLinkRejected) {
		t.Fatalf("already linked elsewhere: %v", err)
	}
}

func TestPendingInvitesCountAndExpire(t *testing.T) {
	s := linkStore(t)
	ctx := context.Background()
	base := RandomToken()
	for i := 0; i < MaxLinkedAccounts; i++ {
		invite(t, s, base, string(rune('a'+i))+"@pending.example")
	}
	if err := s.CreateInvite(ctx, base, "extra@pending.example", RandomToken(), time.Hour); !errors.Is(err, ErrLinkLimit) {
		t.Fatalf("pending invites must count: %v", err)
	}
	// Re-inviting an address replaces its earlier invitation instead of consuming a slot.
	invite(t, s, base, "a@pending.example")
	if pending, err := s.PendingInvites(ctx, base); err != nil || len(pending) != MaxLinkedAccounts {
		t.Fatalf("pending: %d %v", len(pending), err)
	}
	expiring := RandomToken()
	other := RandomToken()
	if err := s.CreateInvite(ctx, other, "late@pending.example", expiring, -time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Invite(ctx, expiring); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired invite readable: %v", err)
	}
	if ok, _ := s.HasPendingInvite(ctx, "late@pending.example"); ok {
		t.Fatal("expired invite is pending")
	}
	emails, err := s.ReapExpiredInvites(ctx, other)
	if err != nil || len(emails) != 1 || emails[0] != "late@pending.example" {
		t.Fatalf("reap: %v %v", emails, err)
	}
}
