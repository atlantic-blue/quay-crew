package features_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cucumber/godog"
)

// The promises scenarios touch the control plane nowhere. What they prove is a property of this
// repository: a change that touches behaviour carries a changelog entry and a scenario, or says in
// the pull request body why it has neither. So they build a real git repository per scenario, make a
// real change on a branch of it, and run the real command continuous integration runs.

// changeOp is one thing a change did to one file, in the words the scenarios use.
type changeOp struct {
	path string
	// what is "edits", "writes" or "deletes". An edit and a deletion both need the file to be there
	// before the change, so the base commit is built from these.
	what string
}

type promisesWorld struct {
	// base is the ref the change is read against, which every scenario but one leaves at main.
	base string
	repo string
	ops  []changeOp
	// body is the pull request body the change was opened with.
	body string
	// said is everything the check printed, and refused is whether it stopped.
	said    string
	refused bool
}

type promisesKey struct{}

func promisesFrom(ctx context.Context) *promisesWorld {
	p, _ := ctx.Value(promisesKey{}).(*promisesWorld)
	return p
}

func (p *promisesWorld) git(args ...string) (string, error) {
	command := exec.Command("git", append([]string{
		"-C", p.repo,
		"-c", "user.name=quay",
		"-c", "user.email=quay@example.invalid",
		"-c", "commit.gpgsign=false",
	}, args...)...)
	out, err := command.CombinedOutput()
	return string(out), err
}

func (p *promisesWorld) write(path, body string) error {
	at := filepath.Join(p.repo, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
		return err
	}
	return os.WriteFile(at, []byte(body), 0o600)
}

// makeTheChange builds the repository the check reads: one commit holding everything the change did
// not create, then a branch holding the change itself.
func (p *promisesWorld) makeTheChange() error {
	if out, err := p.git("init", "-q", "-b", "main", "."); err != nil {
		return fmt.Errorf("git init: %v: %s", err, out)
	}
	// Something in the base commit that no scenario touches, so the repository is never empty.
	if err := p.write("README.md", "a repository made for one scenario\n"); err != nil {
		return err
	}
	for _, op := range p.ops {
		if op.what == "writes" {
			continue
		}
		if err := p.write(op.path, "what was there before the change\n"); err != nil {
			return err
		}
	}
	if out, err := p.git("add", "-A"); err != nil {
		return fmt.Errorf("git add: %v: %s", err, out)
	}
	if out, err := p.git("commit", "-qm", "what the change starts from"); err != nil {
		return fmt.Errorf("git commit: %v: %s", err, out)
	}

	if out, err := p.git("switch", "-q", "-c", "change"); err != nil {
		return fmt.Errorf("git switch: %v: %s", err, out)
	}
	for _, op := range p.ops {
		at := filepath.Join(p.repo, filepath.FromSlash(op.path))
		switch op.what {
		case "writes":
			if err := p.write(op.path, "what the change wrote\n"); err != nil {
				return err
			}
		case "edits":
			if err := p.write(op.path, "what was there before the change\nand what the change did\n"); err != nil {
				return err
			}
		case "deletes":
			if err := os.Remove(at); err != nil {
				return err
			}
		}
	}
	if out, err := p.git("add", "-A"); err != nil {
		return fmt.Errorf("git add: %v: %s", err, out)
	}
	if out, err := p.git("commit", "-qm", "the change"); err != nil {
		return fmt.Errorf("git commit: %v: %s", err, out)
	}
	return nil
}

// check runs the command continuous integration runs, over the repository just built.
func (p *promisesWorld) check() error {
	bodyFile := filepath.Join(filepath.Dir(p.repo), "body.md")
	if err := os.WriteFile(bodyFile, []byte(p.body), 0o600); err != nil {
		return err
	}
	command := exec.Command("go", "run", "../cmd/promises",
		"-repo", p.repo, "-base", p.base, "-body", bodyFile)
	out, err := command.CombinedOutput()
	p.said = string(out)
	p.refused = err != nil
	return nil
}

func initializePromisesSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		base, err := os.MkdirTemp("", "quaycrew-promises")
		if err != nil {
			return ctx, err
		}
		p := &promisesWorld{repo: filepath.Join(base, "repository"), base: "main"}
		if err := os.MkdirAll(p.repo, 0o755); err != nil {
			return ctx, err
		}
		return context.WithValue(ctx, promisesKey{}, p), nil
	})

	sc.Step(`^a change that edits "([^"]*)"$`, func(ctx context.Context, path string) error {
		p := promisesFrom(ctx)
		p.ops = append(p.ops, changeOp{path: path, what: "edits"})
		return nil
	})

	sc.Step(`^it writes "([^"]*)"$`, func(ctx context.Context, path string) error {
		p := promisesFrom(ctx)
		p.ops = append(p.ops, changeOp{path: path, what: "writes"})
		return nil
	})

	sc.Step(`^it deletes "([^"]*)"$`, func(ctx context.Context, path string) error {
		p := promisesFrom(ctx)
		p.ops = append(p.ops, changeOp{path: path, what: "deletes"})
		return nil
	})

	sc.Step(`^the pull request body says "([^"]*)"$`, func(ctx context.Context, body string) error {
		promisesFrom(ctx).body = body
		return nil
	})

	sc.Step(`^the check reads the change$`, func(ctx context.Context) error {
		p := promisesFrom(ctx)
		if err := p.makeTheChange(); err != nil {
			return err
		}
		return p.check()
	})

	sc.Step(`^it lets the change through$`, func(ctx context.Context) error {
		p := promisesFrom(ctx)
		if p.refused {
			return fmt.Errorf("the check refused a change that keeps its promises:\n%s", p.said)
		}
		return nil
	})

	sc.Step(`^the check refuses$`, func(ctx context.Context) error {
		p := promisesFrom(ctx)
		if !p.refused {
			return fmt.Errorf("the check let it through:\n%s", p.said)
		}
		return nil
	})

	sc.Step(`^the check refuses, naming "([^"]*)"$`, func(ctx context.Context, path string) error {
		p := promisesFrom(ctx)
		if !p.refused {
			return fmt.Errorf("the check let it through:\n%s", p.said)
		}
		if !strings.Contains(p.said, path) {
			return fmt.Errorf("it refused without saying which file made this a behaviour change:\n%s", p.said)
		}
		return nil
	})

	sc.Step(`^it says the change carries no "([^"]*)"$`, func(ctx context.Context, promise string) error {
		p := promisesFrom(ctx)
		if !strings.Contains(p.said, promise) {
			return fmt.Errorf("it never said %q:\n%s", promise, p.said)
		}
		return nil
	})

	sc.Step(`^the check reads a range that holds nothing$`, func(ctx context.Context) error {
		p := promisesFrom(ctx)
		if err := p.makeTheChange(); err != nil {
			return err
		}
		// The change against itself, which is what a wrong base ref produces: a real repository, a
		// real command, and nothing between the two refs.
		p.base = "change"
		return p.check()
	})

	sc.Step(`^it says it read no files at all$`, func(ctx context.Context) error {
		p := promisesFrom(ctx)
		if !strings.Contains(p.said, "holds no files") {
			return fmt.Errorf("it refused for some other reason:\n%s", p.said)
		}
		return nil
	})

	sc.Step(`^it also edits "([^"]*)"$`, func(ctx context.Context, path string) error {
		p := promisesFrom(ctx)
		p.ops = append(p.ops, changeOp{path: path, what: "edits"})
		return nil
	})

	sc.Step(`^it says an entry is its own file now$`, func(ctx context.Context) error {
		p := promisesFrom(ctx)
		if !strings.Contains(p.said, "CHANGELOG.md") || !strings.Contains(p.said, "its own file under changelog.d") {
			return fmt.Errorf("it never says where an entry goes now:\n%s", p.said)
		}
		return nil
	})

	sc.Step(`^it prints the line that would say why there is none$`, func(ctx context.Context) error {
		p := promisesFrom(ctx)
		if !strings.Contains(p.said, "No changelog entry:") {
			return fmt.Errorf("it refused without saying how to say why:\n%s", p.said)
		}
		return nil
	})
}
