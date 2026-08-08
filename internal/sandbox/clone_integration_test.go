//go:build integration

package sandbox_test

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/atlantic-blue/quay-crew/internal/sandbox"
)

// TestASessionActuallyClonesOnce proves the part the doubles cannot: the real script puts a real checkout
// in the session's working directory, beside the memory file that is already there, and running it again
// leaves the session's own work alone.
//
// The remote is a bare repository made inside the container, so this needs no network and no credential.
// What it does not cover is a private clone over https, which needs a real token against a real host.
func TestASessionActuallyClonesOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	image := os.Getenv("QC_TEST_SANDBOX_IMAGE")
	if image == "" {
		t.Skip("set QC_TEST_SANDBOX_IMAGE to an image with git in it")
	}

	box, err := sandbox.DockerProvider{Image: image}.Create(ctx,
		sandbox.Config{ID: "cloneonce" + strings.Repeat("0", 14)})
	if err != nil {
		t.Fatalf("create the sandbox: %v", err)
	}
	defer func() { _ = box.Close(ctx) }()

	// Something to clone, and a working directory that already holds the memory file, which is the whole
	// reason a clone cannot go into the working directory itself.
	setup := strings.Join([]string{
		"set -e",
		"mkdir -p /tmp/seed && cd /tmp/seed && git init -q",
		"echo first > a.txt && git add a.txt",
		"git -c user.name=a -c user.email=a@b commit -q -m first",
		"git clone -q --bare /tmp/seed /tmp/origin",
		"mkdir -p " + sandbox.WorkingPath,
		"echo 'the memory file' > " + sandbox.WorkingPath + "/CLAUDE.md",
	}, "; ")
	if said := execOutput(t, ctx, box, "sh", "-c", setup); strings.TrimSpace(said) != "" {
		t.Logf("setting up said: %s", said)
	}

	// The real script, with the remote as its first argument the way the crew passes it.
	into := sandbox.WorkingPath + "/origin"
	clone := func() {
		t.Helper()
		proc, err := box.Exec(ctx, sandbox.Spec{
			Argv: []string{"sh", "-c", sandbox.CloneScript(), "sh", "/tmp/origin", into},
		})
		if err != nil {
			t.Fatalf("run the clone: %v", err)
		}
		_, _ = io.Copy(io.Discard, proc.Stdout())
		if err := proc.Wait(); err != nil {
			t.Fatalf("the clone failed: %v: %s", err, proc.Stderr())
		}
	}

	clone()
	if got := execOutput(t, ctx, box, "cat", into+"/a.txt"); strings.TrimSpace(got) != "first" {
		t.Fatalf("the checkout reads %q, want the file that was in the repository", got)
	}
	// Still there, so the clone did not need the working directory to be empty.
	if got := execOutput(t, ctx, box, "cat", sandbox.WorkingPath+"/CLAUDE.md"); !strings.Contains(got, "the memory file") {
		t.Fatalf("the memory file reads %q, so the clone disturbed the working directory", got)
	}

	// A second turn asks again, because the crew asks every turn. Whatever the session has done in the
	// checkout has to survive that.
	if said := execOutput(t, ctx, box, "sh", "-c", "echo mine > "+into+"/local-work.txt"); strings.TrimSpace(said) != "" {
		t.Logf("writing the session's own work said: %s", said)
	}
	clone()
	if got := execOutput(t, ctx, box, "cat", into+"/local-work.txt"); strings.TrimSpace(got) != "mine" {
		t.Fatalf("asking again threw away the session's own work: %q", got)
	}
}
