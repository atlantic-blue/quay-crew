package features_test

import (
	"context"
	"fmt"
	"strings"

	"github.com/cucumber/godog"
)

// firstSystemBuildThatSays is what the tool must name when the system answers with no build at all. It
// is written here as well as in the tool, so a change to either has to be a change to both.
const firstSystemBuildThatSays = "27 August 2026"

// Steps for the scenarios about which build each part of a system is.
//
// The system's build is stamped into the control plane binary, so a scenario sets it on the system it
// stands up and then runs the real tool against it. The tool's own build is stamped into the binary
// the harness builds, which is what makes the two comparable at all.

func initializeVersionSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the system was built from "([^"]*)"$`, func(ctx context.Context, build string) error {
		return systemBuiltFrom(ctx, build, "")
	})

	sc.Step(`^the system does not say which build it is$`, func(ctx context.Context) error {
		return systemBuiltFrom(ctx, "", "")
	})

	sc.Step(`^the sandbox image was made from "([^"]*)"$`, func(ctx context.Context, build string) error {
		w := worldFrom(ctx)
		return systemBuiltFrom(ctx, w.info.Version, build)
	})

	sc.Step(`^the system and the sandbox image were built from the same build as the tool$`,
		func(ctx context.Context) error {
			return systemBuiltFrom(ctx, toolBuild, toolBuild)
		})

	sc.Step(`^the caller asks the tool for the version$`, func(ctx context.Context) error {
		return runTool(ctx, "version")
	})

	sc.Step(`^the caller lists the workspaces$`, func(ctx context.Context) error {
		return runTool(ctx, "workspace", "list")
	})

	// The manual talks to nothing, so it is the command that proves the check never holds one up.
	sc.Step(`^the caller asks the tool for the manual$`, func(ctx context.Context) error {
		return runTool(ctx, "manual")
	})

	sc.Step(`^standard output names the build of the tool$`, func(ctx context.Context) error {
		return says("standard output", toolFrom(ctx).stdout, toolBuild)
	})

	sc.Step(`^standard output names the build of the system$`, func(ctx context.Context) error {
		return says("standard output", toolFrom(ctx).stdout, worldFrom(ctx).info.Version)
	})

	sc.Step(`^standard output names the build of the sandbox image$`, func(ctx context.Context) error {
		w, t := worldFrom(ctx), toolFrom(ctx)
		if w.info.SandboxBuild == "" {
			return says("standard output", t.stdout, "sandbox image")
		}
		return says("standard output", t.stdout, w.info.SandboxBuild)
	})

	sc.Step(`^standard output says the tool and the system are different builds$`, func(ctx context.Context) error {
		return says("standard output", toolFrom(ctx).stdout, "the tool and the system are different builds")
	})

	sc.Step(`^standard output says the sandbox image is a different build$`, func(ctx context.Context) error {
		return says("standard output", toolFrom(ctx).stdout, "sandbox image")
	})

	sc.Step(`^standard output names both of those builds$`, func(ctx context.Context) error {
		w, t := worldFrom(ctx), toolFrom(ctx)
		named := strings.Count(t.stdout, toolBuild)
		if named < 2 {
			return fmt.Errorf("standard output names the tool's build %d times, want it in the listing and in the sentence: %q",
				named, t.stdout)
		}
		other := w.info.SandboxBuild
		if other == "" {
			other = w.info.Version
		}
		if strings.Count(t.stdout, other) < 2 {
			return fmt.Errorf("standard output does not name %q in both the listing and the sentence: %q",
				other, t.stdout)
		}
		return nil
	})

	sc.Step(`^standard output says nothing about a difference$`, func(ctx context.Context) error {
		if got := toolFrom(ctx).stdout; strings.Contains(got, "different build") {
			return fmt.Errorf("standard output reports a difference between parts of one build: %q", got)
		}
		return nil
	})

	sc.Step(`^standard output says the build of the system is unknown$`, func(ctx context.Context) error {
		return says("standard output", toolFrom(ctx).stdout, "unknown")
	})

	sc.Step(`^standard output names the build that first reports it$`, func(ctx context.Context) error {
		return says("standard output", toolFrom(ctx).stdout, firstSystemBuildThatSays)
	})

	sc.Step(`^standard error says the tool and the system are different builds$`, func(ctx context.Context) error {
		t := toolFrom(ctx)
		if err := says("standard error", t.stderr, "the tool and the system are different builds"); err != nil {
			return err
		}
		if err := says("standard error", t.stderr, toolBuild); err != nil {
			return err
		}
		return says("standard error", t.stderr, worldFrom(ctx).info.Version)
	})

	sc.Step(`^standard output carries the answer and nothing about builds$`, func(ctx context.Context) error {
		w, t := worldFrom(ctx), toolFrom(ctx)
		if err := says("standard output", t.stdout, w.workspaceName); err != nil {
			return err
		}
		for _, leaked := range []string{"different build", toolBuild, w.info.Version} {
			if strings.Contains(t.stdout, leaked) {
				return fmt.Errorf("standard output carries %q, which belongs on standard error: %q", leaked, t.stdout)
			}
		}
		return nil
	})

	sc.Step(`^standard error says nothing about builds$`, func(ctx context.Context) error {
		if got := toolFrom(ctx).stderr; strings.Contains(got, "different build") {
			return fmt.Errorf("standard error reports drift against a system it never reached: %q", got)
		}
		return nil
	})
}

// systemBuiltFrom stands the system up again saying it is a different build.
//
// The build is stamped into the binary at build time, so it is decided when the process starts, and a
// scenario changes it the only way anything can: by starting the control plane again. The store is
// the same one, so nothing the scenario has already made is lost.
func systemBuiltFrom(ctx context.Context, system, sandboxImage string) error {
	w := worldFrom(ctx)
	w.info.Version, w.info.SandboxBuild = system, sandboxImage
	if err := w.restart(); err != nil {
		return err
	}
	// The address the tool dialled belongs to the control plane that has just been stopped.
	return listenForTool(ctx)
}
