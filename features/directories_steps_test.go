package features_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/cucumber/godog"
)

// Steps for the command that turns an address into a directory.
//
// They run the real tool in its own process, because the shape of the answer is what is specified: a
// path alone on the first line is what makes `cd "$(krewe where acme)"` work, and a line that is one
// line inside the test process can be two on the caller's screen.
//
// The directory is then checked against what a sandbox binds, read from the same call the container
// runtime is given, rather than against a path this file builds. A path assembled correctly and
// mounted nowhere is exactly the failure this scenario exists to catch.

func initializeDirectorySteps(sc *godog.ScenarioContext) {
	sc.Step(`^the caller asks where "([^"]*)" is$`, func(ctx context.Context, address string) error {
		return runTool(ctx, "where", address)
	})

	sc.Step(`^the directory it names exists on the machine$`, func(ctx context.Context) error {
		named := theDirectoryNamed(ctx)
		info, err := os.Stat(named)
		if err != nil {
			return fmt.Errorf("the command named %q, which is not on the machine: %w", named, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("the command named %q, which is not a directory", named)
		}
		return nil
	})

	sc.Step(`^it says a session reads that directory at "([^"]*)"$`, func(ctx context.Context, mount string) error {
		return says("standard output", toolFrom(ctx).stdout, mount)
	})

	sc.Step(`^the first line is a path and nothing else$`, func(ctx context.Context) error {
		first := theDirectoryNamed(ctx)
		if first == "" {
			return fmt.Errorf("the command said nothing")
		}
		if strings.ContainsAny(first, " \t") {
			return fmt.Errorf("the first line is %q, so it cannot be typed into a shell", first)
		}
		if !strings.HasPrefix(first, "/") {
			return fmt.Errorf("the first line is %q, want an absolute path", first)
		}
		return nil
	})

	sc.Step(`^a file called "([^"]*)" is put in that directory by hand$`, func(ctx context.Context, name string) error {
		named := theDirectoryNamed(ctx)
		return os.WriteFile(filepath.Join(named, name), []byte("a picture"), 0o666)
	})

	// The half a printed path cannot prove about itself: that a sandbox binds this directory, and at
	// the mount point the answer promised.
	sc.Step(`^a sandbox of that workspace binds that directory at "([^"]*)"$`, func(ctx context.Context, mount string) error {
		w := worldFrom(ctx)
		mounts, err := w.storage.Prepare(sandbox.Config{
			ID: "a-session", Workspace: w.workspaceID, Project: "a-project",
		})
		if err != nil {
			return fmt.Errorf("what a sandbox is given: %w", err)
		}
		named := theDirectoryNamed(ctx)
		for _, one := range mounts {
			if one.Source == named {
				if one.Target != mount {
					return fmt.Errorf("a sandbox binds %q at %q, and the answer said %q", named, one.Target, mount)
				}
				return nil
			}
		}
		return fmt.Errorf("the command named %q, and a sandbox binds %v", named, sources(mounts))
	})

	sc.Step(`^the file is inside the directory that sandbox binds$`, func(ctx context.Context) error {
		named := theDirectoryNamed(ctx)
		entries, err := os.ReadDir(named)
		if err != nil {
			return fmt.Errorf("read %q: %w", named, err)
		}
		if len(entries) == 0 {
			return fmt.Errorf("%q is empty, so the file went somewhere else", named)
		}
		return nil
	})
}

// theDirectoryNamed is the path the command printed, which is its whole first line.
func theDirectoryNamed(ctx context.Context) string {
	line, _, _ := strings.Cut(toolFrom(ctx).stdout, "\n")
	return strings.TrimSpace(line)
}

// sources is what a sandbox would bind, for a failure that says what was there instead.
func sources(mounts []sandbox.Mount) []string {
	out := make([]string, 0, len(mounts))
	for _, one := range mounts {
		out = append(out, one.Source+" -> "+one.Target)
	}
	return out
}
