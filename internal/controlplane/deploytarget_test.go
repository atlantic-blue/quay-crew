package controlplane_test

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A whole target, used by every case below that needs one that holds.
func aTarget() *quaycrewv1.DeployTarget {
	return &quaycrewv1.DeployTarget{
		Account:  "123456789012",
		Region:   "eu-west-2",
		Identity: "arn:aws:iam::123456789012:role/quay-deploy",
	}
}

func TestAProjectSaysWhereItDeploys(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	ctx := context.Background()
	workspaceID, projectID := newProject(t, s)

	fresh, err := s.GetProject(ctx, &quaycrewv1.GetProjectRequest{Id: projectID})
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if fresh.GetProject().GetDeployTarget() != nil {
		t.Fatal("a project nobody has told already deploys somewhere")
	}

	set, err := s.SetDeployTarget(ctx, &quaycrewv1.SetDeployTargetRequest{
		Project: projectID, Target: aTarget(),
	})
	if err != nil {
		t.Fatalf("SetDeployTarget: %v", err)
	}
	if got := set.GetProject().GetDeployTarget().GetIdentity(); got != aTarget().GetIdentity() {
		t.Fatalf("the answer says the identity is %q", got)
	}

	// The row a person reads. This is the whole point: the question is answered by a listing rather
	// than by asking whoever remembers.
	listed, err := s.ListProjects(ctx, &quaycrewv1.ListProjectsRequest{Workspace: workspaceID})
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(listed.GetProjects()) != 1 {
		t.Fatalf("the workspace has %d projects, want 1", len(listed.GetProjects()))
	}
	target := listed.GetProjects()[0].GetDeployTarget()
	if target.GetAccount() != "123456789012" || target.GetRegion() != "eu-west-2" {
		t.Fatalf("the listed project deploys to %v", target)
	}
}

func TestATargetIsRefusedBeforeItIsWritten(t *testing.T) {
	for name, target := range map[string]*quaycrewv1.DeployTarget{
		"an account that is not one": {
			Account: "atlantic-blue", Region: "eu-west-2",
			Identity: "arn:aws:iam::123456789012:role/quay-deploy",
		},
		"a region that is not one": {
			Account: "123456789012", Region: "england",
			Identity: "arn:aws:iam::123456789012:role/quay-deploy",
		},
		"an identity from another account": {
			Account: "123456789012", Region: "eu-west-2",
			Identity: "arn:aws:iam::999999999999:role/quay-deploy",
		},
		"half a target": {Account: "123456789012", Region: "eu-west-2"},
	} {
		t.Run(name, func(t *testing.T) {
			s := newServer(&model.FakeRunner{})
			ctx := context.Background()
			_, projectID := newProject(t, s)

			_, err := s.SetDeployTarget(ctx, &quaycrewv1.SetDeployTargetRequest{
				Project: projectID, Target: target,
			})
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("SetDeployTarget returned %v, want InvalidArgument", err)
			}

			// A refusal that half wrote the row is worse than one that wrote nothing: the project
			// then answers "where does this go" with something nobody agreed to.
			after, err := s.GetProject(ctx, &quaycrewv1.GetProjectRequest{Id: projectID})
			if err != nil {
				t.Fatalf("GetProject: %v", err)
			}
			if after.GetProject().GetDeployTarget() != nil {
				t.Fatalf("a refused target was written anyway: %v", after.GetProject().GetDeployTarget())
			}
		})
	}
}

// A wrong account recorded is worse than none, so the door that wrote it opens the other way.
func TestAProjectCanStopDeployingAnywhere(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	ctx := context.Background()
	_, projectID := newProject(t, s)

	if _, err := s.SetDeployTarget(ctx, &quaycrewv1.SetDeployTargetRequest{
		Project: projectID, Target: aTarget(),
	}); err != nil {
		t.Fatalf("SetDeployTarget: %v", err)
	}
	cleared, err := s.SetDeployTarget(ctx, &quaycrewv1.SetDeployTargetRequest{Project: projectID})
	if err != nil {
		t.Fatalf("clearing the target: %v", err)
	}
	if cleared.GetProject().GetDeployTarget() != nil {
		t.Fatalf("a cleared project still deploys to %v", cleared.GetProject().GetDeployTarget())
	}
}

func TestSayingWhereAMissingProjectDeploysIsNotFound(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, err := s.SetDeployTarget(context.Background(), &quaycrewv1.SetDeployTargetRequest{
		Project: "ghost", Target: aTarget(),
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("SetDeployTarget on a missing project returned %v, want NotFound", err)
	}
}

// The refusal has to name the account it read, or the operator is left comparing two long addresses
// by eye to find which of the two they pasted wrong.
func TestTheRefusalNamesBothAccounts(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	ctx := context.Background()
	_, projectID := newProject(t, s)

	_, err := s.SetDeployTarget(ctx, &quaycrewv1.SetDeployTargetRequest{
		Project: projectID,
		Target: &quaycrewv1.DeployTarget{
			Account: "123456789012", Region: "eu-west-2",
			Identity: "arn:aws:iam::999999999999:role/quay-deploy",
		},
	})
	if err == nil {
		t.Fatal("an identity from another account was accepted")
	}
	for _, want := range []string{"123456789012", "999999999999"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %s: %v", want, err)
		}
	}
}
