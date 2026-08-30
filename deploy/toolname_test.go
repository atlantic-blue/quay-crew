package deploy

import (
	"os"
	"strings"
	"testing"
)

// The name the tool used to have has to be installed wherever the tool is installed, otherwise the
// refusal it carries never reaches anybody. A shell answers a missing command with "command not
// found", which reads as a broken install rather than as a rename, and nothing says the word moved.
//
// These read the two places that put a binary on a path: the makefile, for the operator's machine,
// and the sandbox image, for a session. Neither runs anything, so what they prove is that the line is
// there. What the binary does is proved in cmd/quay and against the built binaries in cmd/krewe.

func TestTheImageCarriesTheNameTheToolUsedToHave(t *testing.T) {
	image := theSandboxImage(t)

	if !strings.Contains(image, "go build -o /out/quay ./cmd/quay") {
		t.Error("the image never builds the old name, so a session that types it is told the command does not exist")
	}
	if !strings.Contains(image, "COPY --from=tool /out/quay /usr/local/bin/quay") {
		t.Error("the image builds the old name and never puts it on the path")
	}
}

func TestInstallingTheToolInstallsTheNameItUsedToHave(t *testing.T) {
	body, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatalf("reading the makefile: %v", err)
	}
	makefile := string(body)

	for _, line := range []string{
		`go build -ldflags "-X main.version=$(VERSION)" -o "$$dir/krewe" ./cmd/krewe`,
		`go build -o "$$dir/quay" ./cmd/quay`,
	} {
		if !strings.Contains(makefile, line) {
			t.Errorf("the tool target does not run %s, so that binary is never installed", line)
		}
	}

	// The directory is taken from an existing quay when there is no krewe yet, which is every machine
	// on the day it upgrades. Installed anywhere else, the old binary stays on the path and keeps
	// driving a system that has moved.
	if !strings.Contains(makefile, "command -v krewe 2>/dev/null || command -v quay 2>/dev/null") {
		t.Error("the tool target never looks for an existing quay, so the first upgrade leaves the old " +
			"binary on the path and installs the refusal somewhere the shell does not read")
	}
}
