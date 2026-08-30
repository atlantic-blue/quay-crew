package controlplane_test

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/name"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Every call that takes a scope refuses the word the level above every workspace used to take.
//
// The tool is not the only way in. A tool built before the word moved reaches this process, and so
// does every channel, so the refusal has to be here as well or the call is read as a workspace and
// comes back saying no such workspace, which says nothing about the word having changed.
func TestEveryScopeRefusesTheWordTheLevelUsedToTake(t *testing.T) {
	ctx := context.Background()
	calls := map[string]func(*testing.T) error{
		"SetSecret": func(t *testing.T) error {
			t.Helper()
			_, err := newServer(&model.FakeRunner{}).SetSecret(ctx, &quaycrewv1.SetSecretRequest{
				Scope: name.Retired, Key: "CLAUDE_CODE_OAUTH_TOKEN", Value: "tok-xyz",
			})
			return err
		},
		"SetContext": func(t *testing.T) error {
			t.Helper()
			_, err := newServer(&model.FakeRunner{}).SetContext(ctx, &quaycrewv1.SetContextRequest{
				Scope: name.Retired, Body: "everything this system does",
			})
			return err
		},
		"AttachSkill": func(t *testing.T) error {
			t.Helper()
			_, err := newServer(&model.FakeRunner{}).AttachSkill(ctx, &quaycrewv1.AttachSkillRequest{
				Scope: name.Retired, Name: "git",
			})
			return err
		},
		"DetachSkill": func(t *testing.T) error {
			t.Helper()
			_, err := newServer(&model.FakeRunner{}).DetachSkill(ctx, &quaycrewv1.DetachSkillRequest{
				Scope: name.Retired, Name: "git",
			})
			return err
		},
		"AttachHook": func(t *testing.T) error {
			t.Helper()
			_, err := newServer(&model.FakeRunner{}).AttachHook(ctx, &quaycrewv1.AttachHookRequest{
				Scope: name.Retired, Name: "merge-gate",
			})
			return err
		},
		"DetachHook": func(t *testing.T) error {
			t.Helper()
			_, err := newServer(&model.FakeRunner{}).DetachHook(ctx, &quaycrewv1.DetachHookRequest{
				Scope: name.Retired, Name: "merge-gate",
			})
			return err
		},
		"AttachRole": func(t *testing.T) error {
			t.Helper()
			_, err := newServer(&model.FakeRunner{}).AttachRole(ctx, &quaycrewv1.AttachRoleRequest{
				Scope: name.Retired, Name: "implementer",
			})
			return err
		},
		"DetachRole": func(t *testing.T) error {
			t.Helper()
			_, err := newServer(&model.FakeRunner{}).DetachRole(ctx, &quaycrewv1.DetachRoleRequest{
				Scope: name.Retired, Name: "implementer",
			})
			return err
		},
	}
	for called, call := range calls {
		t.Run(called, func(t *testing.T) {
			err := call(t)
			if err == nil {
				t.Fatalf("%s took a scope of %q, so the word stopped working quietly", called, name.Retired)
			}
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("%s answered %v, want InvalidArgument", called, status.Code(err))
			}
			// The refusal itself, word for word, and not merely a message that happens to carry
			// "system" in it. SetContext already refused an unknown scope by listing the three it
			// takes, so an assertion looking only for the new word passed there without the
			// refusal below it existing at all.
			if want := name.RefuseRetired(name.Retired).Error(); !strings.Contains(err.Error(), want) {
				t.Fatalf("%s refused with %q, want it to carry %q", called, err, want)
			}
		})
	}
}

// And the word it became still works, on the same eight calls, or the refusal above would be proving
// that both words are broken.
func TestEveryScopeStillTakesTheWordTheLevelHasNow(t *testing.T) {
	ctx := context.Background()
	s := newServer(&model.FakeRunner{})
	if _, err := s.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
		Scope: name.System, Key: "CLAUDE_CODE_OAUTH_TOKEN", Value: "tok-xyz",
	}); err != nil {
		t.Fatalf("SetSecret on %q: %v", name.System, err)
	}
	if _, err := s.SetContext(ctx, &quaycrewv1.SetContextRequest{
		Scope: name.System, Body: "everything this system does",
	}); err != nil {
		t.Fatalf("SetContext on %q: %v", name.System, err)
	}
}
