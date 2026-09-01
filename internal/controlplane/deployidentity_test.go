package controlplane

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/skill"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// A fresh system, seeded from the skills this build ships, and a job dispatched into it. Nobody
// imports anything, nobody attaches anything and no secret is set, which is the state every system
// is in on its first day.
//
// The pull request that started this shipped six resources the deploy user could not create. The
// rule that would have caught it is only worth anything if it is in front of a job that never asked
// for it, so this drives the real control plane, the real store and the real skills directory, and
// says what a session is actually given.
func TestASessionIsGivenTheDeployIdentityRuleWithoutAnybodyAttachingIt(t *testing.T) {
	dir := t.TempDir()
	storage := sandbox.Storage{Dir: dir, Host: dir}
	provider := &sandbox.FakeProvider{}
	server := NewServer(Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "ok"},
		Provider: provider, Secrets: secrets.NewMemory(), Storage: storage,
	})
	ctx := context.Background()

	// The directory the image carries, so a manifest that stopped loading fails here.
	server.Seed(ctx, "../../skills", slog.New(slog.DiscardHandler))

	workspace, err := server.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: "atlantic-blue"})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	project, err := server.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
		Workspace: workspace.GetWorkspace().GetId(), Name: "transcript",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if _, err := server.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: project.GetProject().GetId(), Text: "write the terraform for the transcript service",
	}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	// The line the model reads when the conversation starts, which is what makes the brief reachable
	// without anybody naming it.
	body, found := sandbox.ReadMemory(filepath.Join(storage.Dir, "workspaces", workspace.GetWorkspace().GetId(), "claude"))
	if !found {
		t.Fatal("the session was given no memory file at all")
	}
	if !strings.Contains(body, "deploy-identity") {
		t.Errorf("nothing in the memory file names the deploy-identity skill, so a job writing infrastructure has to be told:\n%s", body)
	}
	briefPath := skill.BriefPathIn(sandbox.SkillsPath, "deploy-identity")
	if !strings.Contains(body, briefPath) {
		t.Errorf("the memory file does not say where the brief is (%s), so the rule is a name with nothing behind it", briefPath)
	}

	// And the brief is actually in the container, mounted where the index says, read only: a skill a
	// session is told about and cannot open is worse than one it was never told about.
	if len(provider.Created) == 0 {
		t.Fatal("no sandbox was made")
	}
	want := skill.DirIn(sandbox.SkillsPath, "deploy-identity")
	mounted := false
	for _, mount := range provider.Created[0].Mounts {
		if mount.Target != want {
			continue
		}
		mounted = true
		if !mount.ReadOnly {
			t.Error("the deploy-identity skill is mounted writable, so a session can edit the rule it is held to")
		}
	}
	if !mounted {
		t.Errorf("the sandbox does not mount %s: %+v", want, provider.Created[0].Mounts)
	}
}

// The workspace holds no cloud credential, which is the state the skill has to survive: naming the
// AWS secrets would leave it out of the session entirely, and the rule would be missing in exactly
// the workspaces whose pipeline authenticates by federated identity.
func TestTheDeployIdentityRuleReachesAWorkspaceWithNoCloudCredential(t *testing.T) {
	dir := t.TempDir()
	server := NewServer(Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "ok"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
		Storage: sandbox.Storage{Dir: dir, Host: dir},
	})
	ctx := context.Background()
	server.Seed(ctx, "../../skills", slog.New(slog.DiscardHandler))

	workspace, err := server.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: "atlantic-blue"})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	project, err := server.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
		Workspace: workspace.GetWorkspace().GetId(), Name: "transcript",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	dispatched, err := server.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: project.GetProject().GetId(), Text: "write the terraform for the transcript service",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	listed, err := server.ListSkills(ctx, &quaycrewv1.ListSkillsRequest{Session: dispatched.GetId()})
	if err != nil {
		t.Fatalf("ListSkills: %v", err)
	}
	for _, held := range listed.GetSkills() {
		if held.GetName() != "deploy-identity" {
			continue
		}
		if reason := held.GetLeftOut(); reason != "" {
			t.Fatalf("the session was not given the rule: %s", reason)
		}
		return
	}
	t.Fatalf("the session holds no deploy-identity skill: %+v", listed.GetSkills())
}
