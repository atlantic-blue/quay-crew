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

// The whole path, with nothing standing in for anything: an operator mounts a secret, dispatches a
// task, and the file is in the container the daemon actually made.
//
// The scenarios prove what the crew asks a sandbox to do, and the sandbox tests prove a real daemon
// honours it. Neither says the two meet, and the join is where this kind of feature fails: a write
// that runs before the container exists, or after the task it was for, passes both halves and
// delivers nothing.
//
// The file is read with docker exec rather than through the crew's own handle, because asking the
// crew whether it did the thing is not evidence that it did.
func TestAMountedSecretReachesARealContainer(t *testing.T) {
	image := os.Getenv("QC_TEST_SANDBOX_IMAGE")
	if image == "" {
		t.Skip("set QC_TEST_SANDBOX_IMAGE to the sandbox image")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

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

	const contents = "[user]\n\tname = operator\n\temail = operator@example.com\n"
	if _, err := server.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
		Workspace:  workspace.GetWorkspace().GetId(),
		Key:        "gitconfig",
		Value:      contents,
		Projection: quaycrewv1.SecretProjection_SECRET_PROJECTION_FILE,
	}); err != nil {
		t.Fatalf("mount the secret: %v", err)
	}

	dispatched, err := server.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: project.GetProject().GetId(), Text: "hello",
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	container := sandbox.ContainerName(dispatched.GetId())
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", container).Run() })

	said, err := exec.CommandContext(ctx, "docker", "exec", container,
		"cat", sandbox.SecretFilePath("gitconfig")).Output()
	if err != nil {
		t.Fatalf("the container has no file at %s: %v", sandbox.SecretFilePath("gitconfig"), err)
	}
	if string(said) != contents {
		t.Fatalf("the container holds %q, want the bytes that were mounted", said)
	}

	// And not in the environment as well, which is the exposure mounting it was for. Read from the
	// daemon the same way anybody with access to it would.
	inspected, err := exec.CommandContext(ctx, "docker", "inspect",
		"--format", "{{range .Config.Env}}{{println .}}{{end}}", container).Output()
	if err != nil {
		t.Fatalf("inspect the container: %v", err)
	}
	if strings.Contains(string(inspected), "gitconfig") || strings.Contains(string(inspected), "operator@example.com") {
		t.Fatalf("the value is readable through docker inspect:\n%s", inspected)
	}
}
