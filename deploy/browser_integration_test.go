//go:build integration

package deploy

import (
	"strings"
	"testing"
)

// The image runs the browser it says it runs, on the same terms as the model runtime beside it: a
// pin the registry quietly ignores reads exactly like a pin that works.
func TestTheImageRunsThePlaywrightItPins(t *testing.T) {
	image := sandboxImageUnderTest(t)
	pinned := defaultOf(theSandboxImage(t), "PLAYWRIGHT_VERSION")
	if pinned == "" {
		t.Fatal("PLAYWRIGHT_VERSION has no default, so the image installs whatever is latest today")
	}

	// The command answers "Version 1.62.1", so the version is the last word.
	reported := whatTheImageSays(t, image, "playwright", "--version")
	fields := strings.Fields(reported)
	running := fields[len(fields)-1]
	if running != pinned {
		t.Errorf("the Dockerfile pins Playwright %s and the image runs %q", pinned, reported)
	}
}
