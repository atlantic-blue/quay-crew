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
// The list lives in one place, rebuild, so an operator has one command to type and this has one
// place to check. Upgrade has to reach it, and rebuild has to name everything, so the next thing the
// repository builds cannot be left out of an upgrade the same way.
func TestUpgradeBuildsEverythingASessionRuns(t *testing.T) {
	recipe := target(t, "upgrade")
	// The line that runs it, not the word. The recipe also says "rebuilding" out loud, so a match on
	// the word alone passed on an upgrade that had stopped calling it and listed the parts itself.
	if !strings.Contains(recipe, "$(MAKE) --no-print-directory rebuild") {
		t.Errorf("make upgrade never runs rebuild, so it builds none of what a session runs:\n%s", recipe)
	}
	if !strings.Contains(recipe, "up --build") {
		t.Errorf("make upgrade never rebuilds the stack:\n%s", recipe)
	}

	built := prerequisites(t, "rebuild")
	for _, one := range []string{"tool", "hooks", "sandbox-image"} {
		if !strings.Contains(built, one) {
			t.Errorf("make rebuild never builds %s, so an upgrade leaves it on the build before:\n%s",
				one, built)
		}
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

// prerequisites returns what a target is built from: everything after the colon on its own line.
// A target that only gathers others has no recipe at all, so reading the recipe would find nothing
// and report it as a target that builds nothing.
func prerequisites(t *testing.T, name string) string {
	t.Helper()
	contents, err := os.ReadFile(makefile)
	if err != nil {
		t.Fatalf("reading the Makefile: %v", err)
	}
	for _, line := range strings.Split(string(contents), "\n") {
		rest, found := strings.CutPrefix(line, name+":")
		if !found {
			continue
		}
		if strings.TrimSpace(rest) == "" {
			t.Fatalf("the %s target is built from nothing", name)
		}
		return strings.TrimSpace(rest)
	}
	t.Fatalf("the Makefile has no %s target at all", name)
	return ""
}

// TestUpgradePutsTheSessionsDownBeforeTakingTheirContainers.
//
// The upgrade removes every sandbox container by name from the daemon. A container removed that way
// takes the task in flight with it, and the operator reads "model: run exited: exit status 137, and
// it said nothing about why" against a conversation they were watching. Draining first asks the crew
// to stop each session, so the row says stopped and the sandbox is closed rather than ripped out.
//
// The order is the whole of it: a drain after the containers are gone is a drain of nothing.
func TestUpgradePutsTheSessionsDownBeforeTakingTheirContainers(t *testing.T) {
	recipe := target(t, "upgrade")

	drain := strings.Index(recipe, "--no-print-directory drain")
	if drain < 0 {
		t.Fatalf("make upgrade never drains, so it takes tasks away from under sessions:\n%s", recipe)
	}
	sweep := strings.Index(recipe, "docker rm -f")
	if sweep < 0 {
		t.Fatalf("make upgrade no longer clears sandbox containers at all:\n%s", recipe)
	}
	if drain > sweep {
		t.Errorf("make upgrade drains after removing the containers, so there is nothing left to put "+
			"down cleanly:\n%s", recipe)
	}
	// The tool has to be the one that knows how to drain. Draining with the copy from before the
	// upgrade asks an older binary for a command it may not have.
	built := strings.Index(recipe, "--no-print-directory tool")
	if built < 0 || built > drain {
		t.Errorf("make upgrade drains before it builds the tool, so it drains with the build from "+
			"before:\n%s", recipe)
	}
}

// TestDrainingRefusesTheUpgradeAndSaysHowToGoOverIt. A refusal an operator cannot act on is a
// refusal they work around by hand, and by hand is the raw removal this exists to replace.
func TestDrainingRefusesTheUpgradeAndSaysHowToGoOverIt(t *testing.T) {
	recipe := target(t, "drain")

	if !strings.Contains(recipe, "quay drain") {
		t.Fatalf("the drain target does not ask the crew to put anything down:\n%s", recipe)
	}
	if !strings.Contains(recipe, "FORCE") {
		t.Errorf("a refused drain does not say how to upgrade over a task in flight:\n%s", recipe)
	}
	// A machine with no quay on its path cannot drain, and stopping the upgrade over that would leave
	// it with no way to upgrade at all.
	if !strings.Contains(recipe, "command -v quay") {
		t.Errorf("the drain target assumes quay is on the path, so an upgrade without it fails:\n%s", recipe)
	}
}
