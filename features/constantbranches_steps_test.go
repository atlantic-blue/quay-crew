package features_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cucumber/godog"
)

// The constant branch scenarios touch the control plane nowhere. What they prove is a property of
// this repository: a branch whose condition is a boolean literal does not reach the default branch.
// So they write a real package into a real directory per scenario, and run the real command
// continuous integration runs over it.

type guardWorld struct {
	root string
	// said is everything the guard printed, and refused is whether it stopped.
	said    string
	refused bool
}

type guardKey struct{}

func guardFrom(ctx context.Context) *guardWorld {
	g, _ := ctx.Value(guardKey{}).(*guardWorld)
	return g
}

// write puts one Go file in the scenario's directory, under a package the guard will parse.
func (g *guardWorld) write(source string) error {
	return os.WriteFile(filepath.Join(g.root, "subject.go"), []byte(source), 0o600)
}

func initializeConstantBranchesSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		root, err := os.MkdirTemp("", "quaycrew-constantbranches")
		if err != nil {
			return ctx, err
		}
		return context.WithValue(ctx, guardKey{}, &guardWorld{root: root}), nil
	})

	sc.Step(`^a package whose source is "(.*)"$`, func(ctx context.Context, body string) error {
		g := guardFrom(ctx)
		// The scenario writes one function on one line, so the line the guard names is line 3 every
		// time and a scenario can assert on it.
		return g.write("package subject\n\n" + body + "\n")
	})

	sc.Step(`^a package whose comment is "(.*)"$`, func(ctx context.Context, comment string) error {
		g := guardFrom(ctx)
		return g.write("package subject\n\n// " + comment + "\nfunc f() int { return 0 }\n")
	})

	sc.Step(`^a package whose string holds the words "(.*)"$`, func(ctx context.Context, words string) error {
		g := guardFrom(ctx)
		// The words go in as a quoted Go string. The guard parses, so this is data rather than a
		// branch, which is the whole reason no directory of tests is excluded from it.
		return g.write("package subject\n\nconst fixture = " + strconv.Quote(words) + "\n")
	})

	sc.Step(`^a directory holding no Go source$`, func(ctx context.Context) error {
		g := guardFrom(ctx)
		return os.WriteFile(filepath.Join(g.root, "README.md"), []byte("nothing to parse here\n"), 0o600)
	})

	sc.Step(`^the guard reads the source$`, func(ctx context.Context) error {
		g := guardFrom(ctx)
		command := exec.Command("go", "run", "../cmd/constantbranches", "-root", g.root)
		out, err := command.CombinedOutput()
		g.said = string(out)
		g.refused = err != nil
		return nil
	})

	sc.Step(`^the guard refuses$`, func(ctx context.Context) error {
		g := guardFrom(ctx)
		if !g.refused {
			return fmt.Errorf("the guard let it through, saying: %s", g.said)
		}
		return nil
	})

	sc.Step(`^the guard lets the source through$`, func(ctx context.Context) error {
		g := guardFrom(ctx)
		if g.refused {
			return fmt.Errorf("the guard refused, saying: %s", g.said)
		}
		return nil
	})

	sc.Step(`^it names the file and the line$`, func(ctx context.Context) error {
		g := guardFrom(ctx)
		if !strings.Contains(g.said, "subject.go:3:") {
			return fmt.Errorf("it never named the file and the line, saying: %s", g.said)
		}
		return nil
	})

	sc.Step(`^it says what to write instead$`, func(ctx context.Context) error {
		g := guardFrom(ctx)
		if !strings.Contains(g.said, "Delete the branch") {
			return fmt.Errorf("it never said what to write instead, saying: %s", g.said)
		}
		return nil
	})

	sc.Step(`^it says how many files it read$`, func(ctx context.Context) error {
		g := guardFrom(ctx)
		if !strings.Contains(g.said, "1 files read") {
			return fmt.Errorf("it never said how many files it read, saying: %s", g.said)
		}
		return nil
	})

	sc.Step(`^it says it read no Go source$`, func(ctx context.Context) error {
		g := guardFrom(ctx)
		if !strings.Contains(g.said, "read no Go source") {
			return fmt.Errorf("it never said it read nothing, saying: %s", g.said)
		}
		return nil
	})
}
