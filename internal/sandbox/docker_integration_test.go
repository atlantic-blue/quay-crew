//go:build integration

package sandbox_test

import (
	"context"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/skill"
)

// TestDockerProvider creates a real per session container, execs a command inside it, reads the
// output back, and tears it down, validating the Docker backend end to end.
func TestDockerProvider(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	box, err := sandbox.DockerProvider{Image: "busybox:latest"}.Create(ctx, sandbox.Config{ID: "itest-1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = box.Close(context.Background()) })

	proc, err := box.Exec(ctx, sandbox.Spec{Argv: []string{"echo", "hi from the sandbox"}})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	out, err := io.ReadAll(proc.Stdout())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := proc.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if strings.TrimSpace(string(out)) != "hi from the sandbox" {
		t.Fatalf("stdout = %q, want 'hi from the sandbox'", string(out))
	}

	if err := box.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestDockerProviderKeepsStateAcrossContainers is the assertion this whole design exists for: a
// session writes something, its container is destroyed, a new container is created for the same
// session, and what it wrote is still there. Before the state was mounted in, removing the container
// destroyed the conversation the database still held a handle to, permanently.
func TestDockerProviderKeepsStateAcrossContainers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// Dir and Host are the same path because this test runs on the host, not inside the control
	// plane's container, so the two views of the directory are one directory.
	data := t.TempDir()
	provider := sandbox.DockerProvider{Image: "busybox:latest", Storage: sandbox.Storage{Dir: data, Host: data}}
	config := sandbox.Config{ID: "itest-durable", Workspace: "ws-durable", Project: "prj-durable"}

	first, err := provider.Create(ctx, config)
	if err != nil {
		t.Fatalf("create the first sandbox: %v", err)
	}
	t.Cleanup(func() { _ = first.Close(context.Background()) })

	// Write into both mounts: the conversation store the model keeps, and the project's files.
	for _, dir := range []string{sandbox.ConversationPath, sandbox.WorkingPath} {
		proc, err := first.Exec(ctx, sandbox.Spec{Argv: []string{"sh", "-c", "echo remembered > " + dir + "/note"}})
		if err != nil {
			t.Fatalf("write into %s: %v", dir, err)
		}
		if _, err := io.ReadAll(proc.Stdout()); err != nil {
			t.Fatalf("read the write's output: %v", err)
		}
		if err := proc.Wait(); err != nil {
			t.Fatalf("write into %s exited: %v", dir, err)
		}
	}

	if err := first.Close(ctx); err != nil {
		t.Fatalf("destroy the first sandbox: %v", err)
	}

	second, err := provider.Create(ctx, config)
	if err != nil {
		t.Fatalf("create the replacement sandbox: %v", err)
	}
	t.Cleanup(func() { _ = second.Close(context.Background()) })

	for _, dir := range []string{sandbox.ConversationPath, sandbox.WorkingPath} {
		proc, err := second.Exec(ctx, sandbox.Spec{Argv: []string{"cat", dir + "/note"}})
		if err != nil {
			t.Fatalf("read %s back: %v", dir, err)
		}
		out, err := io.ReadAll(proc.Stdout())
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if err := proc.Wait(); err != nil {
			t.Fatalf("%s did not survive its container: %v", dir, err)
		}
		if strings.TrimSpace(string(out)) != "remembered" {
			t.Fatalf("%s/note reads %q after the container was replaced, want 'remembered'", dir, string(out))
		}
	}

	// A different session in the same project gets its own working directory and shares the
	// conversation store. Two conversations in one working directory means one of them changing a
	// file under the other, and it leaves no level below the project to say anything at.
	//
	// This used to assert the opposite, that siblings shared the working directory. The decision: "give each
	// session its own working directory".
	sibling, err := provider.Create(ctx, sandbox.Config{ID: "itest-sibling", Workspace: config.Workspace, Project: config.Project})
	if err != nil {
		t.Fatalf("create a sibling session's sandbox: %v", err)
	}
	t.Cleanup(func() { _ = sibling.Close(context.Background()) })

	if out := execOutput(t, ctx, sibling, "sh", "-c", "cat "+sandbox.WorkingPath+"/note 2>&1 || true"); strings.Contains(out, "remembered") {
		t.Fatalf("the sibling session reads the other one's working directory: %q", out)
	}
	if out := execOutput(t, ctx, sibling, "cat", sandbox.ConversationPath+"/note"); strings.TrimSpace(out) != "remembered" {
		t.Fatalf("the sibling session reads %q from the conversation store, want it shared", out)
	}
}

// TestDockerProviderDeliversEnv proves the mechanism the subscription token rides on: a value put in
// Spec.Env reaches the process running inside the sandbox. This needs only Docker, so it runs in CI
// and guards the token delivery without any subscription.
func TestDockerProviderDeliversEnv(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	box, err := sandbox.DockerProvider{Image: "busybox:latest"}.Create(ctx, sandbox.Config{ID: "itest-env"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = box.Close(context.Background()) })

	proc, err := box.Exec(ctx, sandbox.Spec{
		Argv: []string{"printenv", "CLAUDE_CODE_OAUTH_TOKEN"},
		Env:  []string{"CLAUDE_CODE_OAUTH_TOKEN=tok-from-secret"},
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	out, err := io.ReadAll(proc.Stdout())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := proc.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if strings.TrimSpace(string(out)) != "tok-from-secret" {
		t.Fatalf("env in sandbox = %q, want 'tok-from-secret'", string(out))
	}
}

// TestDockerProviderAdoptsAnExistingContainer is what a control plane that has forgotten its
// sandboxes runs into. A session's container name is deterministic, so creating again after a restart
// used to hit the daemon's name conflict and leave that session undispatchable until somebody removed
// the container by hand.
func TestDockerProviderAdoptsAnExistingContainer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	provider := sandbox.DockerProvider{Image: "busybox:latest"}
	cfg := sandbox.Config{ID: "itest-adopt"}

	first, err := provider.Create(ctx, cfg)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = first.Close(context.Background()) })

	again, err := provider.Create(ctx, cfg)
	if err != nil {
		t.Fatalf("creating a second time for the same session: %v", err)
	}
	if out := execOutput(t, ctx, again, "echo", "adopted"); out != "adopted" {
		t.Fatalf("the adopted sandbox says %q, want it to be usable", out)
	}

	// And one that had stopped is started rather than left dead, which is the state a container is in
	// after the host or the daemon restarted under it.
	stopContainer(t, ctx, sandbox.ContainerName(cfg.ID))
	restarted, err := provider.Create(ctx, cfg)
	if err != nil {
		t.Fatalf("creating over a stopped container: %v", err)
	}
	if out := execOutput(t, ctx, restarted, "echo", "started again"); out != "started again" {
		t.Fatalf("the restarted sandbox says %q, want it running", out)
	}
}

func execOutput(t *testing.T, ctx context.Context, box sandbox.Sandbox, argv ...string) string {
	t.Helper()
	proc, err := box.Exec(ctx, sandbox.Spec{Argv: argv})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	out, err := io.ReadAll(proc.Stdout())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := proc.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func stopContainer(t *testing.T, ctx context.Context, name string) {
	t.Helper()
	if out, err := exec.CommandContext(ctx, "docker", "stop", name).CombinedOutput(); err != nil {
		t.Fatalf("stopping %s: %v: %s", name, err, out)
	}
}

// TestASessionCanActuallyCommit is the whole point of an identity, and the only way to know it is
// right is to make a commit in a real container and read the author back.
//
// The environment is what carries it: git reads GIT_AUTHOR_NAME, GIT_AUTHOR_EMAIL and the committer
// pair beside them, and refuses when any is missing rather than guessing. A test that asserted the
// four variables were set would have passed just as happily with the wrong names.
func TestASessionCanActuallyCommit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	image := os.Getenv("QC_TEST_SANDBOX_IMAGE")
	if image == "" {
		t.Skip("set QC_TEST_SANDBOX_IMAGE to an image with git in it")
	}

	provider := sandbox.DockerProvider{Image: image}
	box, err := provider.Create(ctx, sandbox.Config{
		ID: "gitidentity" + strings.Repeat("0", 13),
		Env: []string{
			"GIT_AUTHOR_NAME=A Name", "GIT_AUTHOR_EMAIL=a@example.com",
			"GIT_COMMITTER_NAME=A Name", "GIT_COMMITTER_EMAIL=a@example.com",
		},
	})
	if err != nil {
		t.Fatalf("create the sandbox: %v", err)
	}
	defer func() { _ = box.Close(ctx) }()

	proc, err := box.Exec(ctx, sandbox.Spec{Argv: []string{"sh", "-c",
		"cd /tmp && git init -q repo && cd repo && echo hi > a.txt && git add a.txt && " +
			"git commit -q -m probe && git log --format='%an <%ae>|%cn <%ce>' -1"}})
	if err != nil {
		t.Fatalf("run git in the sandbox: %v", err)
	}
	said, err := io.ReadAll(proc.Stdout())
	if err != nil {
		t.Fatalf("read what git said: %v", err)
	}
	if err := proc.Wait(); err != nil {
		t.Fatalf("git refused to commit: %v: %s", err, proc.Stderr())
	}

	// The author and the committer, both of them, because git names them separately and a session
	// commits as the operator rather than on behalf of somebody else.
	if got := strings.TrimSpace(string(said)); got != "A Name <a@example.com>|A Name <a@example.com>" {
		t.Fatalf("the commit is by %q", got)
	}
}

// A session clones in conversation, so the image has to answer git's credential query itself: the
// helper registered in the system configuration reads GH_TOKEN at the moment git asks. Asked of git
// inside a real container rather than of the files, because a helper that is shipped but never
// registered, or registered under a path nothing ships, passes every read of the Dockerfile.
func TestGitFindsItsCredentialWithoutArguments(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	image := os.Getenv("QC_TEST_SANDBOX_IMAGE")
	if image == "" {
		t.Skip("set QC_TEST_SANDBOX_IMAGE to an image with git in it")
	}

	provider := sandbox.DockerProvider{Image: image}
	box, err := provider.Create(ctx, sandbox.Config{
		ID:  "gitcredential" + strings.Repeat("0", 11),
		Env: []string{"GH_TOKEN=a-token-for-this-test"},
	})
	if err != nil {
		t.Fatalf("create the sandbox: %v", err)
	}
	defer func() { _ = box.Close(ctx) }()

	proc, err := box.Exec(ctx, sandbox.Spec{Argv: []string{"sh", "-c",
		"printf 'protocol=https\nhost=github.com\n\n' | git credential fill"}})
	if err != nil {
		t.Fatalf("ask git for a credential: %v", err)
	}
	said, err := io.ReadAll(proc.Stdout())
	if err != nil {
		t.Fatalf("read what git said: %v", err)
	}
	if err := proc.Wait(); err != nil {
		t.Fatalf("git could not fill the credential: %v: %s", err, proc.Stderr())
	}

	answer := string(said)
	if !strings.Contains(answer, "username=x-access-token") {
		t.Errorf("git answered %q, want the helper's username", answer)
	}
	if !strings.Contains(answer, "password=a-token-for-this-test") {
		t.Errorf("git answered without the token from GH_TOKEN, so a private clone in conversation has nothing to authenticate with")
	}
}

// Every binary a shipped skill declares has to actually be in the image, or the declaration is a
// promise the sandbox breaks on somebody's first exec. One guard over the whole class, so the next
// skill cannot repeat the gap: the set of binaries is read from skills/ itself, and each is asked
// for inside a real container rather than looked for in the Dockerfile.
func TestTheImageCarriesEveryBinaryAShippedSkillDeclares(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	image := os.Getenv("QC_TEST_SANDBOX_IMAGE")
	if image == "" {
		t.Skip("set QC_TEST_SANDBOX_IMAGE to the sandbox image")
	}

	shipped, err := skill.Load("../../skills")
	if err != nil {
		t.Fatalf("loading the shipped skills: %v", err)
	}
	binaries := map[string]bool{}
	for _, one := range shipped {
		for _, binary := range one.Binaries {
			binaries[binary] = true
		}
	}
	if len(binaries) == 0 {
		t.Fatal("no shipped skill declares any binary, so this test proves nothing")
	}

	provider := sandbox.DockerProvider{Image: image}
	box, err := provider.Create(ctx, sandbox.Config{
		ID: "skillbinaries" + strings.Repeat("0", 11),
	})
	if err != nil {
		t.Fatalf("create the sandbox: %v", err)
	}
	defer func() { _ = box.Close(ctx) }()

	for binary := range binaries {
		proc, err := box.Exec(ctx, sandbox.Spec{Argv: []string{"sh", "-c", `command -v -- "$1"`, "sh", binary}})
		if err != nil {
			t.Fatalf("ask for %s in the sandbox: %v", binary, err)
		}
		_, _ = io.Copy(io.Discard, proc.Stdout())
		if err := proc.Wait(); err != nil {
			t.Errorf("a shipped skill declares %s and the image does not carry it: %v: %s", binary, err, proc.Stderr())
		}
	}
}

// TestDockerProviderRemovesByName: stopping a session has to work from a process that never made the
// container, so removal goes by name rather than through a held handle. A container that is not
// there is a remove that already happened, and the exact name shape keeps the compose stack's own
// services out of the stranded listing.
func TestDockerProviderRemovesByName(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	provider := sandbox.DockerProvider{Image: "busybox:latest"}
	id := "0123456789abcdef01234567"
	if _, err := provider.Create(ctx, sandbox.Config{ID: id}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = provider.Remove(context.Background(), id) })

	stranded, err := provider.Stranded(ctx)
	if err != nil {
		t.Fatalf("Stranded: %v", err)
	}
	if !slices.Contains(stranded, id) {
		t.Fatalf("Stranded = %v, want it to hold %s", stranded, id)
	}

	if err := provider.Remove(ctx, id); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := exec.CommandContext(ctx, "docker", "inspect", sandbox.ContainerName(id)).Run(); err == nil {
		t.Fatalf("the container is still there after Remove")
	}
	if err := provider.Remove(ctx, id); err != nil {
		t.Fatalf("Remove of an absent container: %v, want success", err)
	}

	stranded, err = provider.Stranded(ctx)
	if err != nil {
		t.Fatalf("Stranded: %v", err)
	}
	if slices.Contains(stranded, id) {
		t.Fatalf("Stranded still lists %s after Remove", id)
	}
}

// A mounted secret has to be a file the session's own user can read, on a mount that is memory
// backed, and readable by nobody else. Every one of those is a property of the daemon rather than of
// the code, so this asks the daemon.
//
// It ran against the real sandbox image on purpose. A looser image manufactures green here: busybox
// runs as root, so a write into a directory owned by root succeeds there and fails in the image
// sessions actually use, where the user is not root and the mount would have belonged to root.
func TestAMountedSecretIsAFileTheSandboxUserCanReadAndNobodyElseCan(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	image := os.Getenv("QC_TEST_SANDBOX_IMAGE")
	if image == "" {
		t.Skip("set QC_TEST_SANDBOX_IMAGE to the sandbox image")
	}

	provider := sandbox.DockerProvider{Image: image}
	box, err := provider.Create(ctx, sandbox.Config{ID: "secretfile" + strings.Repeat("0", 13)})
	if err != nil {
		t.Fatalf("create the sandbox: %v", err)
	}
	defer func() { _ = box.Close(ctx) }()

	// The same shape the system uses: the value through the environment of the one command, never as an
	// argument, and umask before the write so the file is never briefly readable.
	const contents = "[user]\n\tname = operator\n"
	at := sandbox.SecretFilePath("gitconfig")
	write, err := box.Exec(ctx, sandbox.Spec{
		Argv: []string{"sh", "-c", "set -e\numask 077\nmkdir -p " + sandbox.SecretsPath +
			"\nprintf '%s' \"$QC_SECRET_FILE_VALUE\" > " + at},
		Env: []string{"QC_SECRET_FILE_VALUE=" + contents},
	})
	if err != nil {
		t.Fatalf("write the secret: %v", err)
	}
	_, _ = io.ReadAll(write.Stdout())
	if err := write.Wait(); err != nil {
		t.Fatalf("the sandbox user could not write into %s: %v: %s", sandbox.SecretsPath, err, write.Stderr())
	}

	read, err := box.Exec(ctx, sandbox.Spec{Argv: []string{"sh", "-c",
		"cat " + at + " && echo '|' && stat -c '%a %U' " + at +
			" && echo '|' && stat -f -c %T " + sandbox.SecretsPath}})
	if err != nil {
		t.Fatalf("read the secret back: %v", err)
	}
	said, err := io.ReadAll(read.Stdout())
	if err != nil {
		t.Fatalf("read what the sandbox said: %v", err)
	}
	if err := read.Wait(); err != nil {
		t.Fatalf("the sandbox user could not read the file it was given: %v: %s", err, read.Stderr())
	}

	parts := strings.Split(string(said), "|\n")
	if len(parts) != 3 {
		t.Fatalf("the sandbox said %q, which is not the three answers asked for", said)
	}
	if parts[0] != contents {
		t.Fatalf("the file holds %q, want the bytes it was given", parts[0])
	}
	// Readable and writable by the sandbox user, and nothing to anybody else. The umask is what makes
	// this 600, and a file that arrives readable is one any other process in the container can take.
	if got := strings.TrimSpace(parts[1]); got != "600 agent" {
		t.Fatalf("the file is %q, want it owned by the sandbox user and shut to everybody else", got)
	}
	// Memory backed, so the value never reaches the container's writable layer or the host's disk.
	// The whole reason a mounted credential is safer than one in the environment rests on this.
	if got := strings.TrimSpace(parts[2]); got != "tmpfs" {
		t.Fatalf("%s is a %s, want tmpfs", sandbox.SecretsPath, got)
	}
}

// TestASandboxFromBeforeTheRenameIsFoundAndRemoved asks the daemon the question the unit tests can
// only ask the code. An operator upgrades with sessions up, so their containers carry the retired
// name, and a system that cannot see one does not drain it, does not remove it and starts a second
// container beside it on the next exec, while the first keeps the machine's memory.
//
// The container is made here by name rather than through the provider, because the provider can no
// longer write that name. That is the point: this is the state an upgrade inherits, not a state this
// build can produce.
func TestASandboxFromBeforeTheRenameIsFoundAndRemoved(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	provider := sandbox.DockerProvider{Image: "busybox:latest"}
	id := "0123456789abcdef01234568"
	retired := "quaycrew-" + id
	out, err := exec.CommandContext(ctx, "docker", "run", "-d", "--name", retired,
		"busybox:latest", "sleep", "600").CombinedOutput()
	if err != nil {
		t.Fatalf("start a container under the retired name: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", retired).Run() })

	stranded, err := provider.Stranded(ctx)
	if err != nil {
		t.Fatalf("Stranded: %v", err)
	}
	if !slices.Contains(stranded, id) {
		t.Fatalf("Stranded = %v, and %s is running, so a drain leaves it holding the machine", stranded, retired)
	}

	box, held, err := provider.Existing(ctx, id)
	if err != nil {
		t.Fatalf("Existing: %v", err)
	}
	if !held {
		t.Fatalf("the system reaches into session %s and finds nothing, while %s is up", id, retired)
	}
	if got := execOutput(t, ctx, box, "echo", "reached"); got != "reached" {
		t.Fatalf("the adopted sandbox says %q, want it usable", got)
	}
	if named, says := box.(sandbox.Named); !says || named.Name() != retired {
		t.Fatalf("the sandbox calls itself %v, and an attach opens that name", box)
	}

	// And creating for that session adopts it rather than putting a second container beside it.
	again, err := provider.Create(ctx, sandbox.Config{ID: id})
	if err != nil {
		t.Fatalf("Create over a sandbox from before the rename: %v", err)
	}
	if named, says := again.(sandbox.Named); !says || named.Name() != retired {
		t.Fatalf("a second container was started beside %s", retired)
	}

	if err := provider.Remove(ctx, id); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := exec.CommandContext(ctx, "docker", "inspect", retired).Run(); err == nil {
		t.Fatalf("%s is still running after the session was stopped", retired)
	}
	if err := provider.Remove(ctx, id); err != nil {
		t.Fatalf("Remove of an absent container: %v, want success", err)
	}
}
