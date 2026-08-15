package deploy

import (
	"os"
	"strings"
	"testing"
)

// TestTheSandboxImageShipsGo holds the image to what a session working on this repository needs:
// quay-crew is Go only, so `make fmt`, `make lint` and `go test` all need the toolchain inside the
// sandbox, not just in the stage that builds quay.
func TestTheSandboxImageShipsGo(t *testing.T) {
	dockerfile, err := os.ReadFile("sandbox/claude.Dockerfile")
	if err != nil {
		t.Fatalf("reading the sandbox dockerfile: %v", err)
	}
	image := string(dockerfile)

	if !strings.Contains(image, "COPY --from=tool /usr/local/go /usr/local/go") {
		t.Error("the image never copies Go into the runtime stage, so a session has no toolchain to build or test this repository with")
	}
	if !strings.Contains(image, "/usr/local/go/bin") {
		t.Error("the image never puts Go's bin directory on PATH, so a session's shell cannot find go")
	}

	// PATH has to reach the runtime stage's own environment, not the builder's: an ENV set on the
	// "tool" stage never survives into the final image.
	copied := strings.Index(image, "COPY --from=tool /usr/local/go /usr/local/go")
	dropped := strings.Index(image, "USER agent")
	if copied < 0 || dropped < 0 || copied > dropped {
		t.Error("Go is copied in after the image drops to the sandbox user or not at all, so it either fails to copy or lands somewhere the session cannot reach")
	}
}
