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

// A workspace mounts a signing key, and a commit made in the session it gets verifies.
//
// Two things this proves that nothing else does. That the key is usable where it lands: a mounted
// secret is a file in a memory backed directory owned by the sandbox user, and ssh-keygen refuses a
// key file it does not like the look of. And that git can sign at all, which it could not before the
// image carried ssh-keygen.
//
// So the assertion is a verified commit rather than the presence of a program or a file. Either of
// those would pass on a sandbox where git still refused to sign, and the operator's interest is the
// commit.
//
// Identity is set inline rather than mounted, to keep this test about the key. Where it comes from
// in a real session is TestAnOperatorsConfigurationDecidesWhoCommits.
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
		Workspace:  workspace.GetWorkspace().GetId(),
		Key:        controlplane.SigningKeySecret,
		Value:      private,
		Projection: quaycrewv1.SecretProjection_SECRET_PROJECTION_FILE,
	}); err != nil {
		t.Fatalf("mount the signing key: %v", err)
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
	// holding the public half of the key the workspace mounted, which is how a reader who was not
	// there confirms the commit came from that key and not from any key.
	said := inTheSandbox(ctx, t, container, `set -e
git config --global user.name operator
git config --global user.email operator@example.com
printf 'operator@example.com %s\n' "$QC_TEST_PUBLIC_KEY" > /tmp/allowed_signers
git config --global gpg.ssh.allowedSignersFile /tmp/allowed_signers
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
