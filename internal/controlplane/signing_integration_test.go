//go:build integration

package controlplane_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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

// A workspace holds a signing key, and a commit made in the session it gets verifies.
//
// The crew already wrote the key out and pointed git at it. What it could not do was sign, because
// git makes an ssh format signature by running ssh-keygen and the image did not carry it: every
// commit in every sandbox failed with "cannot run ssh-keygen", whatever the key was.
//
// So the assertion is a verified commit rather than the presence of a program. Checking for the
// binary would pass on an image where git still refused to use it, and the operator's interest is
// the commit, not the package list.
//
// Identity is set here rather than by the crew. A session gets none today, which is its own piece of
// work; this test would fail on "please tell me who you are" long before it reached a signature, and
// that would be a different failure wearing this test's name.
func TestASessionCanMakeACommitThatVerifies(t *testing.T) {
	image := os.Getenv("QC_TEST_SANDBOX_IMAGE")
	if image == "" {
		t.Skip("set QC_TEST_SANDBOX_IMAGE to the sandbox image")
	}
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("this test makes a key with ssh-keygen, which is not on this machine")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	private, public := aSigningKey(t)

	runner, err := model.NewRunner("echo", "")
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
	if _, err := server.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
		Workspace: workspace.GetWorkspace().GetId(),
		Key:       controlplane.SigningKeyEnv,
		Value:     private,
	}); err != nil {
		t.Fatalf("set the signing key: %v", err)
	}

	dispatched, err := server.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: project.GetProject().GetId(), Text: "hello",
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	container := sandbox.ContainerName(dispatched.GetId())
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", container).Run() })

	// Everything from here runs through docker exec, because asking the crew whether the sandbox can
	// sign is not evidence that it can. The signature is checked against an allowed signers file
	// holding the public half of the key the workspace was given, which is how a reader who was not
	// there confirms the commit came from that key and not from any key.
	said := inTheSandbox(ctx, t, container, `set -e
git config --global user.name operator
git config --global user.email operator@example.com
printf 'operator@example.com %s\n' "$QC_TEST_PUBLIC_KEY" > /home/agent/.ssh/allowed_signers
git config --global gpg.ssh.allowedSignersFile /home/agent/.ssh/allowed_signers
mkdir -p /tmp/repo && cd /tmp/repo && git init -q .
echo hello > a.txt
git add a.txt
git commit -q -m "a signed commit"
git log --format=%G? -1`, "QC_TEST_PUBLIC_KEY="+public)

	// G is git's answer for a good signature from a known key. N is what it says when there is no
	// signature at all, which is what an image without ssh-keygen produces once the commit is allowed
	// through, so the difference between the two is the whole point of this test.
	if got := strings.TrimSpace(said); got != "G" {
		t.Fatalf("git says the commit's signature is %q, want G for a good signature", got)
	}
}

// aSigningKey makes a throwaway ed25519 pair and returns the private half in OpenSSH format, the
// shape a workspace's signing key secret holds, and the public half for verification.
func aSigningKey(t *testing.T) (private, public string) {
	t.Helper()
	at := filepath.Join(t.TempDir(), "signing_key")
	made := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-C", "quay-crew test", "-f", at)
	if out, err := made.CombinedOutput(); err != nil {
		t.Fatalf("make a signing key: %v: %s", err, out)
	}
	privateBytes, err := os.ReadFile(at)
	if err != nil {
		t.Fatalf("read the private key: %v", err)
	}
	publicBytes, err := os.ReadFile(at + ".pub")
	if err != nil {
		t.Fatalf("read the public key: %v", err)
	}
	return string(privateBytes), strings.TrimSpace(string(publicBytes))
}

// inTheSandbox runs a script in the container the crew made and returns what it printed. A failure
// carries the container's own stderr, because a shell script that dies halfway says why and the exit
// status does not.
func inTheSandbox(ctx context.Context, t *testing.T, container, script string, env ...string) string {
	t.Helper()
	argv := []string{"exec"}
	for _, each := range env {
		argv = append(argv, "--env", each)
	}
	argv = append(argv, container, "sh", "-c", script)
	ran := exec.CommandContext(ctx, "docker", argv...)
	var complained strings.Builder
	ran.Stderr = &complained
	out, err := ran.Output()
	if err != nil {
		t.Fatalf("the sandbox could not run the script: %v: %s", err, complained.String())
	}
	return string(out)
}
