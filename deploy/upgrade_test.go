package deploy

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The Makefile is a directory up. It lives with the repository rather than with the compose stack,
// and the rules under test here are about what an upgrade builds, which is this stack.
const makefile = "../Makefile"

// TestUpgradeBuildsEverythingASessionRuns.
//
// `make upgrade` fast forwarded the checkout, reinstalled the tool and rebuilt the stack, and never
// touched the sandbox image. So the tool and the control plane moved forward while every session
// carried on in a container from the build before, with a `quay` inside it that was older than the
// crew or was not in the image at all. Nothing said so.
//
// This holds the target to naming every image the repository builds, so the next image added cannot
// be left out of an upgrade the same way.
func TestUpgradeBuildsEverythingASessionRuns(t *testing.T) {
	recipe := target(t, "upgrade")
	for _, built := range []string{"install", "sandbox-image"} {
		if !strings.Contains(recipe, built) {
			t.Errorf("make upgrade never builds %s, so an upgrade leaves it on the build before:\n%s",
				built, recipe)
		}
	}
	if !strings.Contains(recipe, "up --build") {
		t.Errorf("make upgrade never rebuilds the stack:\n%s", recipe)
	}
}

// TestTheSandboxImageRecordsWhichBuildItCameFrom. An image that does not say which build made it
// cannot be told from a current one, and a warning nobody can compute is a warning nobody gets.
func TestTheSandboxImageRecordsWhichBuildItCameFrom(t *testing.T) {
	if recipe := target(t, "sandbox-image"); !strings.Contains(recipe, "QC_VERSION=$(VERSION)") {
		t.Errorf("the sandbox image is built without the build stamped into it:\n%s", recipe)
	}

	contents, err := os.ReadFile("sandbox/claude.Dockerfile")
	if err != nil {
		t.Fatalf("reading the sandbox Dockerfile: %v", err)
	}
	dockerfile := string(contents)
	if !strings.Contains(dockerfile, "LABEL com.quaycrew.build=$QC_VERSION") {
		t.Errorf("the sandbox image carries no build label, so the crew cannot read one back:\n%s", dockerfile)
	}
	// The label alone would leave the tool inside reporting a build it is not. Both come from the
	// same argument, so they cannot disagree.
	if !strings.Contains(dockerfile, "-X main.version=$QC_VERSION") {
		t.Errorf("the quay in the sandbox is built without a version, so it reports one it is not:\n%s", dockerfile)
	}
}

// target returns the recipe of one Makefile target: every line after it that a make recipe is made
// of, which is every line indented with a tab.
func target(t *testing.T, name string) string {
	t.Helper()
	contents, err := os.ReadFile(makefile)
	if err != nil {
		t.Fatalf("reading the Makefile: %v", err)
	}
	lines := strings.Split(string(contents), "\n")
	header := regexp.MustCompile(`^` + regexp.QuoteMeta(name) + `:`)

	var recipe []string
	found := false
	for _, line := range lines {
		if header.MatchString(line) {
			found = true
			continue
		}
		if !found {
			continue
		}
		if !strings.HasPrefix(line, "\t") {
			break
		}
		recipe = append(recipe, line)
	}
	if !found {
		t.Fatalf("the Makefile has no %s target at all", name)
	}
	if len(recipe) == 0 {
		t.Fatalf("the %s target has no recipe", name)
	}
	return strings.Join(recipe, "\n")
}
