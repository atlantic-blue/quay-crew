package secrets_test

import (
	"context"
	"errors"
	"testing"

	"github.com/atlantic-blue/krewe/internal/secrets"
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
// Every secret already in a running system is that secret, so getting this wrong would move all of them
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

// The system's level is a level, not a workspace: what is set on it is read by every workspace, and
// what is set on a workspace stays that workspace's own.
func TestTheSystemsSecretsReachEveryWorkspace(t *testing.T) {
	levels := secrets.Levels{Store: secrets.NewMemory()}
	ctx := context.Background()

	if err := levels.SetSystem(ctx, secrets.Secret{Name: "GITHUB_TOKEN", Value: "ghp-shared"}); err != nil {
		t.Fatalf("SetSystem: %v", err)
	}
	for _, workspace := range []string{"me", "acme", "one-made-later"} {
		got, err := levels.Get(ctx, workspace, "GITHUB_TOKEN")
		if err != nil {
			t.Fatalf("Get for %s: %v", workspace, err)
		}
		if got != "ghp-shared" {
			t.Fatalf("%s reads GITHUB_TOKEN as %q, want ghp-shared", workspace, got)
		}
	}
}

// Without this the system's level would be a floor rather than a default, and the one workspace that
// needs a different token could not have one.
func TestAWorkspaceWinsOnAName(t *testing.T) {
	levels := secrets.Levels{Store: secrets.NewMemory()}
	ctx := context.Background()
	if err := levels.SetSystem(ctx, secrets.Secret{Name: "GITHUB_TOKEN", Value: "ghp-shared"}); err != nil {
		t.Fatalf("SetSystem: %v", err)
	}
	if err := levels.Set(ctx, "me", secrets.Secret{Name: "GITHUB_TOKEN", Value: "ghp-mine"}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if got, _ := levels.Get(ctx, "me", "GITHUB_TOKEN"); got != "ghp-mine" {
		t.Fatalf("me reads GITHUB_TOKEN as %q, want its own ghp-mine", got)
	}
	if got, _ := levels.Get(ctx, "acme", "GITHUB_TOKEN"); got != "ghp-shared" {
		t.Fatalf("acme reads GITHUB_TOKEN as %q, want the system's ghp-shared", got)
	}

	// And one name appears once, held by the workspace, rather than twice with the system's shadowing
	// it further down the listing.
	refs, err := levels.List(ctx, "me")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("me holds %d secrets, want 1: %+v", len(refs), refs)
	}
	if refs[0].System {
		t.Fatalf("the listing says GITHUB_TOKEN is the system's, and this workspace set its own")
	}
}

// A workspace that attached nothing and reads three secrets is a puzzle, so a listing says where
// each one came from.
func TestAListingSaysWhichLevelHoldsEachSecret(t *testing.T) {
	levels := secrets.Levels{Store: secrets.NewMemory()}
	ctx := context.Background()
	if err := levels.SetSystem(ctx, secrets.Secret{Name: "GITHUB_TOKEN", Value: "ghp-shared"}); err != nil {
		t.Fatalf("SetSystem: %v", err)
	}
	if err := levels.Set(ctx, "me", secrets.Secret{Name: "STRIPE_KEY", Value: "sk-mine"}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	refs, err := levels.List(ctx, "me")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// Sorted by name, so two levels read as one listing.
	want := []secrets.Ref{
		{Name: "GITHUB_TOKEN", Projection: secrets.Env, System: true},
		{Name: "STRIPE_KEY", Projection: secrets.Env},
	}
	if len(refs) != len(want) {
		t.Fatalf("me reads %d secrets, want %d: %+v", len(refs), len(want), refs)
	}
	for i, ref := range refs {
		if ref != want[i] {
			t.Fatalf("secret %d is %+v, want %+v", i, ref, want[i])
		}
	}
}

// A credential that is a file is the case that hurts most to repeat, so the system's level carries the
// projection as well as the bytes.
func TestTheSystemCanHoldAMountedCredential(t *testing.T) {
	levels := secrets.Levels{Store: secrets.NewMemory()}
	ctx := context.Background()
	if err := levels.SetSystem(ctx, secrets.Secret{
		Name: "gitconfig", Value: "[user] name = operator", Projection: secrets.File,
	}); err != nil {
		t.Fatalf("SetSystem: %v", err)
	}

	refs, err := levels.List(ctx, "me")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(refs) != 1 || refs[0].Projection != secrets.File {
		t.Fatalf("me reads %+v, want gitconfig mounted as a file", refs)
	}
}

// A name that would escape its directory is refused at the system's level too. One reader of the rule
// was the reason it lives on the secret; a second entry point must not be a second chance to skip it.
func TestTheSystemRefusesAMountedNameThatWouldEscape(t *testing.T) {
	levels := secrets.Levels{Store: secrets.NewMemory()}
	err := levels.SetSystem(context.Background(), secrets.Secret{
		Name: "../../etc/passwd", Value: "root", Projection: secrets.File,
	})
	if err == nil {
		t.Fatal("SetSystem accepted a name that would have become a path")
	}
}
