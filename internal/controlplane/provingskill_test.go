package controlplane_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/controlplane"
	"github.com/atlantic-blue/krewe/internal/model"
	"github.com/atlantic-blue/krewe/internal/sandbox"
	"github.com/atlantic-blue/krewe/internal/secrets"
	"github.com/atlantic-blue/krewe/internal/skill"
	"github.com/atlantic-blue/krewe/internal/store"
)

// shippedSkills is where the skills this build ships live, from this package's directory. The image
// carries the same directory, so a skill that stops reaching a session fails here rather than on
// somebody's first run.
const shippedSkills = "../../skills"

// aSeededSystem is a control plane started the way one starts for real: seeded from the skills this
// build ships with, then given a workspace and a project. Nothing attaches a skill to either.
func aSeededSystem(t *testing.T) (*controlplane.Server, *sandbox.FakeProvider, string) {
	t.Helper()
	dir := t.TempDir()
	provider := &sandbox.FakeProvider{}
	server := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "ok"},
		Provider: provider, Secrets: secrets.NewMemory(),
		Storage: sandbox.Storage{Dir: dir, Host: dir},
	})
	ctx := context.Background()
	server.Seed(ctx, shippedSkills, slog.New(slog.DiscardHandler))

	workspace, err := server.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: "acme"})
	if err != nil {
		t.Fatalf("create the workspace: %v", err)
	}
	project, err := server.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
		Workspace: workspace.GetWorkspace().GetId(), Name: "captions",
	})
	if err != nil {
		t.Fatalf("create the project: %v", err)
	}
	return server, provider, project.GetProject().GetId()
}

// The acceptance criterion, driven over the real control plane: a job that produces a design is
// offered this skill without being told to. Nobody imports it and nobody attaches it. The system is
// seeded, the job names the designer role, and the session doing it holds the skill.
func TestADesigningJobIsOfferedTheProvingSkill(t *testing.T) {
	server, provider, project := aSeededSystem(t)
	ctx := context.Background()

	files := filesOf(t, filepath.Join(shippedRoles, "designer"))
	if _, err := server.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{Files: files}); err != nil {
		t.Fatalf("the system refused the designer role, which ships with it: %v", err)
	}
	read, err := server.GetProject(ctx, &quaycrewv1.GetProjectRequest{Id: project})
	if err != nil {
		t.Fatalf("read the project: %v", err)
	}
	workspace := read.GetProject().GetWorkspace()
	if _, err := server.AttachRole(ctx, &quaycrewv1.AttachRoleRequest{
		Workspace: workspace, Name: "designer",
	}); err != nil {
		t.Fatalf("attach the designer role: %v", err)
	}

	declared, err := server.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "design the captions service",
		Brief: "say how the captions are fetched", Role: "designer",
	})
	if err != nil {
		t.Fatalf("declare the job: %v", err)
	}
	server.TickJob(ctx)

	done, err := server.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: declared.GetJob().GetId()})
	if err != nil {
		t.Fatalf("read the job: %v", err)
	}
	session := done.GetJob().GetSession()
	if session == "" {
		t.Fatalf("the job runs in no session, so nothing was offered anything: %s", done.GetJob().GetReason())
	}

	listed, err := server.ListSkills(ctx, &quaycrewv1.ListSkillsRequest{Session: session})
	if err != nil {
		t.Fatalf("list what the session holds: %v", err)
	}
	var held *quaycrewv1.Skill
	heldNames := make([]string, 0, len(listed.GetSkills()))
	for _, one := range listed.GetSkills() {
		heldNames = append(heldNames, one.GetName())
		if one.GetName() == "proving" {
			held = one
		}
	}
	if held == nil {
		t.Fatalf("the session designing something holds %v, and none of them says to prove the riskiest assumption in the runtime", heldNames)
	}
	if held.GetLeftOut() != "" {
		t.Errorf("the proving skill was left out of the session: %s", held.GetLeftOut())
	}
	if !strings.Contains(strings.ToLower(held.GetSummary()), "runtime") {
		t.Errorf("the line the session reads is %q, and it has to say where the proof belongs", held.GetSummary())
	}

	// Where it came from, which is the half that makes "without being told to" true. The workspace
	// attached nothing, so a skill it holds as its own would mean every workspace has to be set up
	// before a design job in it is offered anything.
	inWorkspace, err := server.ListSkills(ctx, &quaycrewv1.ListSkillsRequest{Workspace: workspace})
	if err != nil {
		t.Fatalf("list what the workspace holds: %v", err)
	}
	fromSystem := false
	for _, one := range inWorkspace.GetSkills() {
		if one.GetName() == "proving" {
			fromSystem = one.GetSystem()
		}
	}
	if !fromSystem {
		t.Error("the workspace holds the proving skill as its own, so a workspace nobody set up would not have it")
	}

	// The brief itself is a file in the container, and the index points at it. A skill listed and not
	// mounted is a session reading about a capability it does not have. The task is detached, so the
	// container is built while this test is already past the listing.
	at := skill.DirIn(sandbox.SkillsPath, "proving")
	mounts := func() bool {
		for _, box := range provider.Configurations() {
			for _, mount := range box.Mounts {
				if mount.Target == at {
					return true
				}
			}
		}
		return false
	}
	deadline := time.Now().Add(5 * time.Second)
	for !mounts() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !mounts() {
		t.Errorf("no sandbox mounts %s, so the brief the index names is not in the container", at)
	}
}
