//go:build integration

package model_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
)

// TestClaudeCodeRunnerRealTurn runs a real Claude turn inside the sandbox image, authenticated by the
// subscription token, and checks a reply and a resumable session id come back.
//
// It needs a subscription, so it cannot run in continuous integration: set CLAUDE_CODE_OAUTH_TOKEN
// (from `claude setup-token`) and build the sandbox image (`make sandbox-image`) to run it. Without
// both it skips, which is why the integration job stays green without a subscription.
func TestClaudeCodeRunnerRealTurn(t *testing.T) {
	token := os.Getenv("CLAUDE_CODE_OAUTH_TOKEN")
	if token == "" {
		t.Skip("set CLAUDE_CODE_OAUTH_TOKEN (from `claude setup-token`) to run the real Claude turn")
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

	box, err := sandbox.DockerProvider{Image: image}.Create(ctx, "claude-itest", nil)
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
		t.Fatalf("run turn: %v", err)
	}
	if strings.TrimSpace(resp.Reply) == "" {
		t.Fatal("empty reply from a real Claude turn")
	}
	if resp.ModelSessionID == "" {
		t.Fatal("no model session id, so the thread could not be resumed")
	}
	t.Logf("reply=%q session=%s", resp.Reply, resp.ModelSessionID)
}
