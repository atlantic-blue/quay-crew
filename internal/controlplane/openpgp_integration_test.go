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

// A workspace mounts an OpenPGP key, and the commit a session makes carries a signature from that
// key.
//
// This is the gpg half of TestASessionCanMakeACommitThatVerifies, and it proves the part that
// nothing else can: a gpg key is not usable where it lands, so the sandbox has to import it into a
// keyring, find the fingerprint and point git at it, and the image has to carry gpg for git to run
// at all. Every step of that is invisible until a commit is made.
//
// The signature is checked against the fingerprint of the key the workspace mounted, so a commit
// signed by some other key in the sandbox could not pass.
func TestASessionCanMakeAnOpenPGPCommitThatVerifies(t *testing.T) {
	image, home := anImageAndAKeyring(t)

	fingerprint := anOpenPGPKey(t, home, "no-passphrase@example.com", "")
	exported := gpgSays(t, home, "", "--armor", "--export-secret-keys", fingerprint)

	said := aCommitSignedWith(t, image, map[string]string{controlplane.OpenPGPKeySecret: exported})
	assertSignedBy(t, said, fingerprint)
}

// The same, for the operator who exports the key as it stands rather than making a copy with the
// passphrase stripped off it. gpg asks for a passphrase through pinentry, and a task nobody is
// watching waits forever, so the passphrase is mounted beside the key and gpg is told to read it
// from there.
func TestASessionSignsWithAKeyThatHasAPassphrase(t *testing.T) {
	image, home := anImageAndAKeyring(t)

	const passphrase = "open sesame"
	fingerprint := anOpenPGPKey(t, home, "passphrase@example.com", passphrase)
	exported := gpgSays(t, home, passphrase, "--armor", "--export-secret-keys", fingerprint)

	said := aCommitSignedWith(t, image, map[string]string{
		controlplane.OpenPGPKeySecret:        exported,
		controlplane.OpenPGPPassphraseSecret: passphrase,
	})
	assertSignedBy(t, said, fingerprint)
}

// anImageAndAKeyring answers with the image under test and a throwaway gpg home to make keys in, or
// skips when this machine cannot run either.
func anImageAndAKeyring(t *testing.T) (image, home string) {
	t.Helper()
	image = os.Getenv("QC_TEST_SANDBOX_IMAGE")
	if image == "" {
		t.Skip("set QC_TEST_SANDBOX_IMAGE to the sandbox image")
	}
	if _, err := exec.LookPath("gpg"); err != nil {
		t.Skip("this test makes a key with gpg, which is not on this machine")
	}
	return image, t.TempDir()
}

// aCommitSignedWith mounts the given secrets on a workspace, dispatches a task to get a sandbox, and
// makes a commit in it. It answers with what git says about that commit's signature.
//
// Identity is set inline rather than mounted, to keep this test about the key. Where it comes from
// in a real session is TestAnOperatorsConfigurationDecidesWhoCommits.
func aCommitSignedWith(t *testing.T, image string, mounted map[string]string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	t.Cleanup(cancel)

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
	for name, value := range mounted {
		if _, err := server.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
			Workspace:  workspace.GetWorkspace().GetId(),
			Key:        name,
			Value:      value,
			Projection: quaycrewv1.SecretProjection_SECRET_PROJECTION_FILE,
		}); err != nil {
			t.Fatalf("mount %s: %v", name, err)
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

	// Everything from here runs through docker exec, because asking the system whether the sandbox can
	// sign is not evidence that it can. The keyring is read back with it: the key is imported into a
	// memory backed directory, so it never reaches the writable layer the daemon keeps on disk.
	return inTheSandbox(ctx, t, container, `set -e
git config --global user.name operator
git config --global user.email operator@example.com
mkdir -p /tmp/repo && cd /tmp/repo && git init -q .
echo hello > a.txt
git add a.txt
git commit -q -m "a signed commit"
git log --format='%G? %GK' -1
printf 'keyring %s\n' "$GNUPGHOME"`)
}

// assertSignedBy holds git's answer to a signature made by exactly the key the workspace mounted,
// and holds the keyring to memory.
func assertSignedBy(t *testing.T, said, fingerprint string) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(said), "\n")
	if len(lines) != 2 {
		t.Fatalf("the sandbox said %q, want a signature line and a keyring line", said)
	}
	// G is a good signature from a key git trusts, U a good signature from one it has no trust
	// setting for, which is what a freshly imported key is. N is what a commit with no signature at
	// all says, and that is what this test is here to fail on.
	status, keyID, found := strings.Cut(lines[0], " ")
	if !found || (status != "G" && status != "U") {
		t.Fatalf("git says the commit's signature is %q, want a good signature", lines[0])
	}
	if keyID == "" || !strings.HasSuffix(fingerprint, keyID) {
		t.Fatalf("the commit was signed by key %q, which is not the mounted key %q", keyID, fingerprint)
	}
	if want := "keyring /dev/shm/"; !strings.HasPrefix(lines[1], want) {
		t.Fatalf("the sandbox keeps its keyring at %q, want it under /dev/shm so the key stays in memory", lines[1])
	}
}

// anOpenPGPKey makes a throwaway key in the given gpg home and answers with its fingerprint. Signing
// only, and no expiry: it lives as long as the temporary directory holding it.
func anOpenPGPKey(t *testing.T, home, address, passphrase string) string {
	t.Helper()
	gpgSays(t, home, passphrase, "--quick-generate-key", "Quay System Test <"+address+">", "ed25519", "sign", "0")
	listed := gpgSays(t, home, passphrase, "--list-secret-keys", "--with-colons", address)
	for _, line := range strings.Split(listed, "\n") {
		if fields := strings.Split(line, ":"); fields[0] == "fpr" && len(fields) > 9 {
			return fields[9]
		}
	}
	t.Fatalf("no fingerprint in what gpg listed:\n%s", listed)
	return ""
}

// gpgSays runs gpg against a throwaway home and answers with what it printed. Loopback, so making
// and exporting a key with a passphrase never reaches for a pinentry program the runner may not have.
func gpgSays(t *testing.T, home, passphrase string, argv ...string) string {
	t.Helper()
	argv = append([]string{"--batch", "--yes", "--quiet", "--pinentry-mode", "loopback", "--passphrase", passphrase}, argv...)
	ran := exec.Command("gpg", argv...)
	ran.Env = append(os.Environ(), "GNUPGHOME="+home)
	var complained strings.Builder
	ran.Stderr = &complained
	out, err := ran.Output()
	if err != nil {
		t.Fatalf("gpg %s: %v: %s", strings.Join(argv, " "), err, complained.String())
	}
	return string(out)
}
