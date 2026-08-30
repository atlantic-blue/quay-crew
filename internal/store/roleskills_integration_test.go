//go:build integration

package store_test

import (
	"context"
	"path"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/job"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/skill"
	"github.com/atlantic-blue/quay-crew/internal/store"
)

// Whether a role can push, decided the only place it is actually decided: the container the system
// built for it.
//
// A push needs the git tool, nothing is cloned for a session, and a repository reaches one here
// through the git skill. So `receives: skills` is the whole of "this role can push", and taking it
// away leaves a session that cannot get its work onto a branch however its brief is worded. The unit
// tier reads that off the manifest. What only this tier reaches is the crossing: the role is a row,
// it is read back by the code that assembles a session, and the mount either exists or it does not.

// aSystemThatHoldsSkills stands the control plane up on a real database, holding the skills this build
// ships, so the git skill in the assertions below is the one an operator actually gets.
func aSystemThatHoldsSkills(t *testing.T) (*controlplane.Server, *sandbox.FakeProvider) {
	t.Helper()
	truncate(t)
	kept, err := store.NewPostgres(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(kept.Close)
	shipped, err := skill.Load("../../skills")
	if err != nil {
		t.Fatalf("loading the skills this build ships: %v", err)
	}
	if len(shipped) == 0 {
		t.Fatal("skills/ holds none, so this test would assert over nothing")
	}
	dir := t.TempDir()
	boxes := &sandbox.FakeProvider{}
	return controlplane.NewServer(controlplane.Config{
		Store: kept, Runner: &model.FakeRunner{Reply: "done"}, Provider: boxes,
		Secrets: secrets.NewMemory(), Storage: sandbox.Storage{Dir: dir, Host: dir},
		Skills: shipped, SkillsHost: "../../skills", SandboxImage: "quaycrew-sandbox:test",
	}), boxes
}

// TestARoleThatMayPushIsHandedTheGitSkillAndOneThatIsNotIsNot runs a job as each of two roles and
// reads what the system put in front of each session.
func TestARoleThatMayPushIsHandedTheGitSkillAndOneThatIsNotIsNot(t *testing.T) {
	for _, one := range []struct {
		what     string
		material []string
		wantGit  bool
	}{
		{
			what:     "a role that receives skills holds the git skill and its mount",
			material: []string{"job", "skills"},
			wantGit:  true,
		},
		{
			what:     "a role that does not receive skills holds neither",
			material: []string{"job", "context"},
			wantGit:  false,
		},
	} {
		t.Run(one.what, func(t *testing.T) {
			s, boxes := aSystemThatHoldsSkills(t)
			ctx := context.Background()
			workspace, project := aProjectOnPostgres(t, s)
			// The git skill names GH_TOKEN, and a skill whose secret is not set is left out of the
			// session rather than handed over broken. Without this the positive case would fail for
			// a reason that has nothing to do with the role.
			if _, err := s.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
				Workspace: workspace, Key: "GH_TOKEN", Value: "a token",
			}); err != nil {
				t.Fatalf("SetSecret: %v", err)
			}
			importRoleOnPostgres(t, s, workspace, "releaser", 1, one.material...)

			declared, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
				Project: project, Title: "release it", Brief: "push the branch and open the pull request",
				Role: "releaser",
			})
			if err != nil {
				t.Fatalf("CreateJob: %v", err)
			}
			done := waitForJob(t, s, declared.GetJob().GetId(), job.PhaseDone)
			if done.GetSession() == "" {
				t.Fatal("the job ran in no session, so there is no container to read")
			}

			// What the system says the session holds.
			listed, err := s.ListSkills(ctx, &quaycrewv1.ListSkillsRequest{Session: done.GetSession()})
			if err != nil {
				t.Fatalf("ListSkills: %v", err)
			}
			held := false
			for _, got := range listed.GetSkills() {
				if got.GetName() == "git" {
					held = true
				}
			}
			if held != one.wantGit {
				t.Errorf("the session holds git: %v, want %v (it holds %d skills)",
					held, one.wantGit, len(listed.GetSkills()))
			}

			// And what is actually in the container, which is the half a listing cannot answer: a
			// listing that says git and a sandbox with no mount is a session that cannot push.
			box, found := sandboxFor(boxes, done.GetSession())
			if !found {
				t.Fatalf("the system built no sandbox for session %s", done.GetSession())
			}
			at := skill.DirIn(sandbox.SkillsPath, "git")
			mounted := false
			for _, mount := range box.Mounts {
				if mount.Target == at {
					mounted = true
					if mount.Source != path.Join("../../skills", "git") {
						t.Errorf("the git skill is mounted from %q", mount.Source)
					}
					if !mount.ReadOnly {
						t.Error("the git skill is mounted writable, and a session may not edit its own capability")
					}
				}
			}
			if mounted != one.wantGit {
				t.Errorf("the sandbox mounts %s: %v, want %v", at, mounted, one.wantGit)
			}
		})
	}
}

// sandboxFor is the configuration the system asked for on behalf of one session.
func sandboxFor(boxes *sandbox.FakeProvider, session string) (sandbox.Config, bool) {
	for _, made := range boxes.Configurations() {
		if made.ID == session {
			return made, true
		}
	}
	return sandbox.Config{}, false
}
