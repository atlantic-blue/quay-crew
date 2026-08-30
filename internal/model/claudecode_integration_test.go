//go:build integration

package model_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/atlantic-blue/krewe/internal/model"
	"github.com/atlantic-blue/krewe/internal/sandbox"
)

// TestClaudeCodeRunnerRealTask runs a real Claude task inside the sandbox image, authenticated by the
// subscription token, and checks a reply and a resumable session id come back.
//
// It needs a subscription, so it cannot run in continuous integration: set CLAUDE_CODE_OAUTH_TOKEN
// (from `claude setup-token`) and build the sandbox image (`make sandbox-image`) to run it. Without
// both it skips, which is why the integration job stays green without a subscription.
func TestClaudeCodeRunnerRealTask(t *testing.T) {
	token := os.Getenv("CLAUDE_CODE_OAUTH_TOKEN")
	if token == "" {
		t.Skip("set CLAUDE_CODE_OAUTH_TOKEN (from `claude setup-token`) to run the real Claude task")
	}

	image := os.Getenv("QC_TEST_SANDBOX_IMAGE")
	if image == "" {
		image = "quaycrew-sandbox-claude:local"
	}
	if err := exec.Command("docker", "image", "inspect", image).Run(); err != nil {
		t.Skipf("sandbox image %s not found; build it with `make sandbox-image`", image)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	box, err := sandbox.DockerProvider{Image: image}.Create(ctx, sandbox.Config{ID: "claude-itest"})
	if err != nil {
		t.Fatalf("create sandbox: %v", err)
	}
	t.Cleanup(func() { _ = box.Close(context.Background()) })

	resp, err := model.NewClaudeCodeRunner().Run(ctx, box, model.Request{
		Text:           "Reply with exactly the word: pong",
		PermissionMode: "plan",
		Env:            map[string]string{model.ClaudeCodeOAuthTokenEnv: token},
	})
	if err != nil {
		t.Fatalf("run task: %v", err)
	}
	if strings.TrimSpace(resp.Reply) == "" {
		t.Fatal("empty reply from a real Claude task")
	}
	if resp.ModelSessionID == "" {
		t.Fatal("no model session id, so the session could not be resumed")
	}
	t.Logf("reply=%q session=%s", resp.Reply, resp.ModelSessionID)
}

// TestClaudeConversationSurvivesItsContainer is the same claim as
// TestDockerProviderKeepsStateAcrossContainers, made against the real model rather than a file: the
// conversation itself, not just the bytes underneath it.
//
// A task happens, the container is destroyed, a new one is created for the same session, and the
// model still remembers what it was told. The transcript lives in the mounted directory rather than
// in the container, so resuming has something to read.
//
// It spends a subscription, so it skips exactly like the real task test above.
func TestClaudeConversationSurvivesItsContainer(t *testing.T) {
	token := os.Getenv("CLAUDE_CODE_OAUTH_TOKEN")
	if token == "" {
		t.Skip("set CLAUDE_CODE_OAUTH_TOKEN (from `claude setup-token`) to run a real conversation")
	}
	image := os.Getenv("QC_TEST_SANDBOX_IMAGE")
	if image == "" {
		image = "quaycrew-sandbox-claude:local"
	}
	if err := exec.Command("docker", "image", "inspect", image).Run(); err != nil {
		t.Skipf("sandbox image %s not found; build it with `make sandbox-image`", image)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	data := t.TempDir()
	provider := sandbox.DockerProvider{Image: image, Storage: sandbox.Storage{Dir: data, Host: data}}
	config := sandbox.Config{ID: "claude-durable", Workspace: "ws-durable", Project: "prj-durable"}
	runner := model.NewClaudeCodeRunner()
	env := map[string]string{model.ClaudeCodeOAuthTokenEnv: token}

	first, err := provider.Create(ctx, config)
	if err != nil {
		t.Fatalf("create the first sandbox: %v", err)
	}
	t.Cleanup(func() { _ = first.Close(context.Background()) })

	opening, err := runner.Run(ctx, first, model.Request{
		Text:           "Remember the number 84713. Reply with exactly: ok",
		PermissionMode: "plan",
		Env:            env,
	})
	if err != nil {
		t.Fatalf("first task: %v", err)
	}
	if opening.ModelSessionID == "" {
		t.Fatal("no conversation id came back, so there is nothing to resume")
	}

	if err := first.Close(ctx); err != nil {
		t.Fatalf("destroy the first sandbox: %v", err)
	}

	second, err := provider.Create(ctx, config)
	if err != nil {
		t.Fatalf("create the replacement sandbox: %v", err)
	}
	t.Cleanup(func() { _ = second.Close(context.Background()) })

	resumed, err := runner.Run(ctx, second, model.Request{
		Text:           "What number did I ask you to remember? Reply with only the number.",
		ModelSessionID: opening.ModelSessionID,
		PermissionMode: "plan",
		Env:            env,
	})
	if err != nil {
		t.Fatalf("resume the conversation in a new container: %v", err)
	}
	if !strings.Contains(resumed.Reply, "84713") {
		t.Fatalf("the resumed conversation replied %q, want it to remember 84713", resumed.Reply)
	}
	t.Logf("conversation %s survived its container: %q", opening.ModelSessionID, strings.TrimSpace(resumed.Reply))
}

// TestClaudeSandboxImageSkipsFirstRunPrompts guards the thing that made attaching useless: a fresh
// sandbox that has never completed the CLI's first run stops at the theme picker and then the
// workspace trust prompt. A task never notices, because a task is not interactive. Attaching does
// nothing else.
//
// Skips unless the image has been built (`make sandbox-image`), the same as the real task test.
func TestClaudeSandboxImageSkipsFirstRunPrompts(t *testing.T) {
	const image = "quaycrew-sandbox-claude:local"
	if exec.Command("docker", "image", "inspect", image).Run() != nil {
		t.Skipf("%s is not built; run make sandbox-image", image)
	}

	out, err := exec.Command("docker", "run", "--rm", image,
		"node", "-e", `const d=require("/home/agent/.claude.json");
			const p = d.projects && d.projects["/home/agent/workspace"] || {};
			console.log([d.hasCompletedOnboarding === true, p.hasTrustDialogAccepted === true].join(","))`,
	).Output()
	if err != nil {
		t.Fatalf("read the seeded config from the image: %v", err)
	}

	if got := strings.TrimSpace(string(out)); got != "true,true" {
		t.Fatalf("the image reports onboarding,trust = %q, want true,true: an attach would stop at a prompt", got)
	}
}
