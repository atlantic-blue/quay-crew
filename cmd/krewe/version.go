package main

import (
	"context"
	"fmt"
	"io"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
)

// firstSystemBuildThatSays is when a system began reporting its own build. A system older than this
// answers with nothing, and an operator reading "unknown" needs to know what to upgrade to rather
// than what to debug.
const firstSystemBuildThatSays = "27 August 2026"

// driftTimeout caps the extra call every command makes. The system is on this machine or one hop away,
// so a second is generous, and a system that is slow to answer must never hold up the command itself.
const driftTimeout = time.Second

// builds is which build each part of a system is. Empty means that part does not say, which is not the
// same as a difference and is never reported as one.
type builds struct {
	tool         string
	system       string
	sandboxImage string
}

// runVersion prints which build each part of the system is, and says where two of them differ.
//
// A system is three parts and each is built on its own: this tool, the control plane, and the image
// every session runs in. An upgrade stops every container, so it gets delayed, and the three drift
// apart with nothing saying so. Three defects were investigated as live on 27 August 2026 and all
// three were fixed already.
func runVersion(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, out io.Writer) error {
	held := builds{tool: version}
	asking, giveUp := context.WithTimeout(ctx, driftTimeout)
	defer giveUp()
	// A system that cannot be reached is not an error here. This command answers what this binary is,
	// which is the one part of the answer that needs nobody.
	if described, err := client.GetInfo(asking, &quaycrewv1.GetInfoRequest{}); err == nil {
		held.system = described.GetVersion()
		held.sandboxImage = described.GetSandboxBuild()
	}

	fmt.Fprintf(out, "tool           %s\n", said(held.tool))
	fmt.Fprintf(out, "system           %s\n", said(held.system))
	fmt.Fprintf(out, "sandbox image  %s\n", said(held.sandboxImage))

	for _, difference := range held.differences() {
		fmt.Fprintf(out, "\n%s\n", difference)
	}
	if held.system == "" {
		fmt.Fprintf(out, "\nthe system does not say which build it is: a system built on or after %s says, "+
			"so this one is older than that. Bring it up to date with make upgrade\n", firstSystemBuildThatSays)
	}
	return nil
}

// said is how a build reads when there is none to print.
func said(build string) string {
	if build == "" {
		return "unknown"
	}
	return build
}

// differences names each pair that came from a different commit. A part that does not say which
// build it is takes no part in this: an unknown build is not a difference, and accusing a good system
// of being behind is worse than saying nothing.
func (b builds) differences() []string {
	pairs := []struct {
		what  string
		one   string
		other string
	}{
		{"the tool and the system", b.tool, b.system},
		{"the tool and the sandbox image", b.tool, b.sandboxImage},
		{"the system and the sandbox image", b.system, b.sandboxImage},
	}
	found := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		if pair.one == "" || pair.other == "" || pair.one == pair.other {
			continue
		}
		found = append(found, fmt.Sprintf("%s are different builds: %s against %s. "+
			"Build them from one commit with make upgrade", pair.what, pair.one, pair.other))
	}
	return found
}

// reportDrift puts one line on the error stream when this tool and the system are different builds.
//
// It goes on every command rather than on this one, because an operator chasing a defect types the
// command the defect is in, not the one that would have told them. Standard error, because standard
// output is where a caller reads data, and one extra line there is a value nobody asked for.
//
// It never refuses a command and it never delays one for long: a system that says nothing, or that
// cannot be reached, is left to the command itself to report.
func reportDrift(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, said io.Writer) {
	asking, giveUp := context.WithTimeout(ctx, driftTimeout)
	defer giveUp()
	described, err := client.GetInfo(asking, &quaycrewv1.GetInfoRequest{})
	if err != nil {
		return
	}
	system := described.GetVersion()
	if system == "" || system == version {
		return
	}
	fmt.Fprintf(said, "krewe: the tool and the system are different builds: %s against %s. "+
		"Build them from one commit with make upgrade\n", version, system)
}
