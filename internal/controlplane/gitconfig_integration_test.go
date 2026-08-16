//go:build integration

package controlplane_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
)

// An operator's own git configuration, the shape most of them actually have: an identity, and
// signing on for everything against a key the machine holds and a container does not.
//
// Both halves of this feature are in that one file. The identity has to arrive, and the signing has
// to be overruled, and neither is any use without the other: a session that knows who it is and
// cannot commit is exactly as stuck as one that does not know who it is.
const anOperatorsConfiguration = `[user]
	name = Operator
	email = operator@example.com
[commit]
	gpgsign = true
[tag]
	gpgsign = true
`

// A session commits, and the commit is the operator's.
//
// The scenarios say what the crew asks a sandbox to do. This says git honours it, which is a
// separate claim and the one that matters: the include has to sit where git reads it, the mounted
// file has to land where the include points, and the crew's own writes have to come after both. Any
// one of those wrong and the scenarios still pass while every commit in every sandbox fails.
//
// So the assertion is a commit that exists and carries the right author, made through docker exec
// rather than through the crew's own handle.
func TestAnOperatorsConfigurationDecidesWhoCommits(t *testing.T) {
	image := os.Getenv("QC_TEST_SANDBOX_IMAGE")
	if image == "" {
		t.Skip("set QC_TEST_SANDBOX_IMAGE to the sandbox image")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	container := aSessionWhoseWorkspaceMounts(ctx, t, image, anOperatorsConfiguration)

	said := inTheSandbox(ctx, t, container, `set -e
mkdir -p /tmp/repo && cd /tmp/repo && git init -q .
echo hello > a.txt
git add a.txt
git commit -q -m "a commit"
git log --format='%an <%ae>|%G?' -1`)

	author, signature, found := strings.Cut(strings.TrimSpace(said), "|")
	if !found {
		t.Fatalf("git said %q, which is not the two answers asked for", said)
	}
	if author != "Operator <operator@example.com>" {
		t.Fatalf("the commit is by %q, want the identity the workspace mounted", author)
	}
	// N is git's answer for a commit with no signature. The mounted configuration asked for one, and
	// this workspace holds no key, so the crew's own answer has to be the one git reads last.
	if signature != "N" {
		t.Fatalf("git says the signature is %q, want N: this workspace holds no key to sign with", signature)
	}
}

// A workspace that mounts nothing is unchanged, which is what makes this safe to ship to a crew that
// has never heard of it. Git treats an include pointing at nothing as no include at all, so the only
// way to find out is to run one.
func TestASessionWhoseWorkspaceMountsNothingIsUnchanged(t *testing.T) {
	image := os.Getenv("QC_TEST_SANDBOX_IMAGE")
	if image == "" {
		t.Skip("set QC_TEST_SANDBOX_IMAGE to the sandbox image")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	container := aSessionWhoseWorkspaceMounts(ctx, t, image, "")

	// git status rather than a config read, because a broken include is not a quiet thing: git
	// refuses to do anything at all in the repository and says so on the error stream.
	said := inTheSandbox(ctx, t, container, `set -e
mkdir -p /tmp/repo && cd /tmp/repo && git init -q .
git status --porcelain
echo READY`)
	if !strings.Contains(said, "READY") {
		t.Fatalf("git said %q in a workspace that mounted nothing, want it working normally", said)
	}
}

// aSessionWhoseWorkspaceMounts makes a crew, mounts the given configuration unless it is empty,
// dispatches a task, and returns the name of the container the crew built for it.
func aSessionWhoseWorkspaceMounts(ctx context.Context, t *testing.T, image, configuration string) string {
	t.Helper()

	runner, err := model.NewRunner("echo", "", "")
	if err != nil {
		t.Fatalf("model runner: %v", err)
	}
	server := controlplane.NewServer(controlplane.Config{
		Store:    store.NewMemory(),
		Runner:   runner,
		Provider: sandbox.DockerProvider{Image: image},
		Secrets:  secrets.NewMemory(),
	})

	workspace, err := server.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: "acme"})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	project, err := server.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
		Workspace: workspace.GetWorkspace().GetId(), Name: "house-bills",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if configuration != "" {
		if _, err := server.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
			Workspace:  workspace.GetWorkspace().GetId(),
			Key:        sandbox.GitConfigSecret,
			Value:      configuration,
			Projection: quaycrewv1.SecretProjection_SECRET_PROJECTION_FILE,
		}); err != nil {
			t.Fatalf("mount the configuration: %v", err)
		}
	}

	dispatched, err := server.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: project.GetProject().GetId(), Text: "hello",
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	container := sandbox.ContainerName(dispatched.GetId())
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", container).Run() })
	return container
}
