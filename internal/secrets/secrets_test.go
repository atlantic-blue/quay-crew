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

	if err := store.Set(ctx, "acme", secrets.Secret{Name: "telegram_token", Value: "s3cret"}); err != nil {
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
	_ = store.Set(ctx, "acme", secrets.Secret{Name: "k", Value: "a"})
	_ = store.Set(ctx, "other", secrets.Secret{Name: "k", Value: "b"})

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
	if err := store.Set(context.Background(), "", secrets.Secret{Name: "k", Value: "v"}); err == nil {
		t.Fatal("Set with empty workspace = nil error, want error")
	}
	if err := store.Set(context.Background(), "acme", secrets.Secret{Value: "v"}); err == nil {
		t.Fatal("Set with no name = nil error, want error")
	}
}

// A secret set before projections existed carries none, and the answer for one is the environment.
// Every secret already in a running crew is that secret, so getting this wrong would move all of them
// out of the environment at once.
func TestNoProjectionIsTheEnvironment(t *testing.T) {
	store := secrets.NewMemory()
	ctx := context.Background()
	if err := store.Set(ctx, "acme", secrets.Secret{Name: "GH_TOKEN", Value: "v"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	refs, err := store.List(ctx, "acme")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(refs) != 1 || refs[0].Projection != secrets.Env {
		t.Fatalf("List = %+v, want one secret projected into the environment", refs)
	}
}

func TestListSaysHowEachOneReachesASandbox(t *testing.T) {
	store := secrets.NewMemory()
	ctx := context.Background()
	_ = store.Set(ctx, "acme", secrets.Secret{Name: "gitconfig", Value: "[user]", Projection: secrets.File})
	_ = store.Set(ctx, "acme", secrets.Secret{Name: "GH_TOKEN", Value: "v", Projection: secrets.Env})

	refs, err := store.List(ctx, "acme")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// Sorted, so a listing does not reorder itself between reads.
	want := []secrets.Ref{
		{Name: "GH_TOKEN", Projection: secrets.Env},
		{Name: "gitconfig", Projection: secrets.File},
	}
	if len(refs) != len(want) {
		t.Fatalf("List = %+v, want %+v", refs, want)
	}
	for i, ref := range refs {
		if ref != want[i] {
			t.Fatalf("List[%d] = %+v, want %+v", i, ref, want[i])
		}
	}
}

// Setting a secret again is how an operator changes which way it reaches a sandbox. A projection that
// outlived the value it described would send the new value somewhere nobody asked for.
func TestSettingAgainMovesWhereItLands(t *testing.T) {
	store := secrets.NewMemory()
	ctx := context.Background()
	_ = store.Set(ctx, "acme", secrets.Secret{Name: "creds", Value: "a", Projection: secrets.File})
	_ = store.Set(ctx, "acme", secrets.Secret{Name: "creds", Value: "b", Projection: secrets.Env})

	refs, _ := store.List(ctx, "acme")
	if len(refs) != 1 || refs[0].Projection != secrets.Env {
		t.Fatalf("List = %+v, want creds projected into the environment", refs)
	}
}

// A mounted name becomes a file name inside a sandbox, so one that walks out of its own directory is
// refused at the store rather than at the moment of writing.
func TestAMountedNameCannotEscapeItsDirectory(t *testing.T) {
	store := secrets.NewMemory()
	for _, name := range []string{"../escape", "a/b", `a\b`, ".", ".."} {
		err := store.Set(context.Background(), "acme", secrets.Secret{
			Name: name, Value: "v", Projection: secrets.File,
		})
		if err == nil {
			t.Fatalf("Set mounted %q = nil error, want a refusal", name)
		}
	}
	// The same names are fine in the environment, where nothing opens a path with them.
	if err := store.Set(context.Background(), "acme", secrets.Secret{Name: "a/b", Value: "v"}); err != nil {
		t.Fatalf("Set a/b into the environment: %v", err)
	}
}
