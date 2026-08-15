//go:build integration

package deploy

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// The image runs the version it says it runs.
//
// A pin the registry quietly ignores reads exactly like a pin that works: the Dockerfile names a
// version, the build succeeds, and the image holds something else. So the number in the file is a
// claim, and this is the check of it. It asks the built image rather than the Dockerfile, which is
// the only place the answer actually lives.
func TestTheImageRunsTheClaudeCodeItPins(t *testing.T) {
	image := sandboxImageUnderTest(t)
	pinned := defaultOf(theSandboxImage(t), "CLAUDE_CODE_VERSION")
	if pinned == "" {
		t.Fatal("CLAUDE_CODE_VERSION has no default, so the image installs whatever is latest today")
	}

	// The command answers "2.1.233 (Claude Code)", so the version is the first word. Matched exactly
	// rather than looked for anywhere in the line, because 2.1.233 is inside 2.1.2330 as well.
	reported := whatTheImageSays(t, image, "claude", "--version")
	running, _, _ := strings.Cut(reported, " ")
	if running != pinned {
		t.Errorf("the Dockerfile pins Claude Code %s and the image runs %q", pinned, reported)
	}
}

func sandboxImageUnderTest(t *testing.T) string {
	t.Helper()
	image := os.Getenv("QC_TEST_SANDBOX_IMAGE")
	if image == "" {
		image = "quaycrew-sandbox-claude:local"
	}
	if err := exec.Command("docker", "image", "inspect", image).Run(); err != nil {
		t.Skipf("sandbox image %s not found; build it with `make sandbox-image`", image)
	}
	return image
}

func whatTheImageSays(t *testing.T, image string, argv ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	run := append([]string{"run", "--rm", image}, argv...)
	said, err := exec.CommandContext(ctx, "docker", run...).CombinedOutput()
	if err != nil {
		t.Fatalf("running %v in %s: %v\n%s", argv, image, err, said)
	}
	return strings.TrimSpace(string(said))
}
