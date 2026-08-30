package deploy

import (
	"os"
	"strings"
	"testing"
)

// A service that cannot say which build it is is the whole of the defect this stamping answers: on
// 27 August 2026 three defects were investigated as live and every one was fixed already, because
// the tool in use was older than the system and nothing said so.
//
// The stamping is invisible from every Go test: the binary under test is built by `go test`, which
// stamps nothing, so a build that quietly stopped passing the commit would report "dev" forever and
// each of those tests would still pass. These read the files that do the stamping.

// serviceDockerfile builds the control plane and the gateway.
const serviceDockerfile = "../Dockerfile"

func TestTheServiceBinaryIsStampedWithTheBuildItCameFrom(t *testing.T) {
	contents, err := os.ReadFile(serviceDockerfile)
	if err != nil {
		t.Fatalf("reading the service Dockerfile: %v", err)
	}
	dockerfile := string(contents)

	if !strings.Contains(dockerfile, "ARG QC_VERSION") {
		t.Fatalf("the Dockerfile takes no QC_VERSION build argument, so nothing can tell it which build it is:\n%s", dockerfile)
	}
	if !strings.Contains(dockerfile, "-X main.version=${QC_VERSION}") {
		t.Fatalf("the Dockerfile does not stamp the build into the binary, so the system reports the default:\n%s", dockerfile)
	}
}

func TestTheComposeStackTellsTheControlPlaneWhichBuildItIs(t *testing.T) {
	contents, err := os.ReadFile("docker-compose.yml")
	if err != nil {
		t.Fatalf("reading the compose file: %v", err)
	}
	compose := string(contents)

	controlplane := strings.Index(compose, "controlplane:")
	if controlplane < 0 {
		t.Fatal("the compose file has no control plane, so this test proves nothing")
	}
	// Only as far as the next service, so a build argument on another one cannot pass this.
	block := compose[controlplane:]
	if next := strings.Index(block, "\n  gateway:"); next > 0 {
		block = block[:next]
	}
	if !strings.Contains(block, "QC_VERSION:") {
		t.Fatalf("the control plane is built without QC_VERSION, so the system cannot say which build it is:\n%s", block)
	}
}

// The commit, not a fixed word. The stack is built from this checkout, so what the system reports has
// to be what the checkout is.
func TestTheUpTargetPassesTheBuildOfThisCheckout(t *testing.T) {
	contents, err := os.ReadFile(makefile)
	if err != nil {
		t.Fatalf("reading the Makefile: %v", err)
	}

	var stamped int
	for _, line := range strings.Split(string(contents), "\n") {
		if !strings.Contains(line, "up --build") {
			continue
		}
		if !strings.Contains(line, "QC_VERSION=$(VERSION)") {
			t.Errorf("this line builds the stack without saying which build it is: %q", strings.TrimSpace(line))
			continue
		}
		stamped++
	}
	if stamped == 0 {
		t.Fatal("no target in the Makefile builds the stack, so this test proves nothing")
	}
}
