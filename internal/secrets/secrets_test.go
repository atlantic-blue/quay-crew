package secrets_test

import (
	"context"
	"errors"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/secrets"
)

func TestMemoryRoundTrip(t *testing.T) {
	store := secrets.NewMemory()
	ctx := context.Background()

	if err := store.Set(ctx, "acme", "telegram_token", "s3cret"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := store.Get(ctx, "acme", "telegram_token")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "s3cret" {
		t.Fatalf("Get = %q, want s3cret", got)
	}
}

func TestMemoryIsWorkspaceScoped(t *testing.T) {
	store := secrets.NewMemory()
	ctx := context.Background()
	_ = store.Set(ctx, "acme", "k", "a")
	_ = store.Set(ctx, "other", "k", "b")

	if v, _ := store.Get(ctx, "acme", "k"); v != "a" {
		t.Fatalf("acme/k = %q, want a", v)
	}
	if v, _ := store.Get(ctx, "other", "k"); v != "b" {
		t.Fatalf("other/k = %q, want b", v)
	}
}

func TestMemoryNotFound(t *testing.T) {
	store := secrets.NewMemory()
	if _, err := store.Get(context.Background(), "acme", "missing"); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("Get missing err = %v, want ErrNotFound", err)
	}
}

func TestMemoryRejectsEmpty(t *testing.T) {
	store := secrets.NewMemory()
	if err := store.Set(context.Background(), "", "k", "v"); err == nil {
		t.Fatal("Set with empty workspace = nil error, want error")
	}
}
