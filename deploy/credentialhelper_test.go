package deploy

import (
	"os"
	"strings"
	"testing"
)

// TestTheSandboxImageShipsTheGitCredentialHelper holds the image to what the git skill relies on: a
// session clones in conversation, so git has to find the token itself. The helper reads GH_TOKEN from
// the environment at the moment git asks, which is what keeps the token out of every argument list and
// every transcript.
func TestTheSandboxImageShipsTheGitCredentialHelper(t *testing.T) {
	dockerfile, err := os.ReadFile("sandbox/claude.Dockerfile")
	if err != nil {
		t.Fatalf("reading the sandbox dockerfile: %v", err)
	}
	image := string(dockerfile)

	if !strings.Contains(image, "deploy/sandbox/git-credential-env") {
		t.Error("the image does not copy the credential helper, so a clone in conversation has no way to authenticate without the token landing in an argument")
	}
	if !strings.Contains(image, "credential.helper") {
		t.Error("the image never configures git to use the helper, so shipping it does nothing")
	}

	// System configuration needs root, and the image drops to the sandbox user partway down.
	configured := strings.Index(image, "credential.helper")
	dropped := strings.Index(image, "USER agent")
	if configured > dropped && dropped >= 0 {
		t.Error("the helper is configured after the image drops to the sandbox user, which cannot write system configuration, so the build fails or the helper is never registered")
	}

	helper, err := os.ReadFile("sandbox/git-credential-env")
	if err != nil {
		t.Fatalf("reading the credential helper: %v", err)
	}
	script := string(helper)

	if !strings.Contains(script, "password=$GH_TOKEN") {
		t.Error("the helper does not answer with GH_TOKEN, so a private clone in conversation fails and the model reaches for the token by hand")
	}
	if !strings.Contains(script, "username=") {
		t.Error("the helper gives no username, and a password with no username is refused by the host")
	}
	// git calls a helper for get, store and erase. Answering anything but get would echo the token
	// back at operations that are not asking for it.
	if !strings.Contains(script, "get") {
		t.Error("the helper does not restrict itself to git's get operation")
	}
}
